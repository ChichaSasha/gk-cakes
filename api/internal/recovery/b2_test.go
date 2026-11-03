package recovery

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
)

const (
	fakeKeyID  = "0031234"
	fakeKey    = "K003secretapplicationkey"
	fakeBucket = "gk-cakes-sandbox-backups-test"
)

// fakeB2 models the B2 native API v4 calls the journal uses.
type fakeB2 struct {
	t            *testing.T
	mu           sync.Mutex
	srv          *httptest.Server
	files        map[string][]byte
	capabilities []string
	namePrefix   any // string or nil
	buckets      []map[string]any
	authCalls    int
	uploadURLs   int
	// Failure injection, consumed once each.
	expireAPIToken    bool
	expireUploadToken bool
	uploadUnavailable bool
	wrongSha1         bool
}

func newFakeB2(t *testing.T) *fakeB2 {
	f := &fakeB2{
		t:            t,
		files:        map[string][]byte{},
		capabilities: []string{"listFiles", "readFiles", "writeFiles"},
		namePrefix:   "recovery/",
		buckets:      []map[string]any{{"id": "bucket-1", "name": fakeBucket}},
	}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeB2) journal(t *testing.T) *B2 {
	t.Helper()
	j, err := NewB2(B2Config{KeyID: fakeKeyID, Key: fakeKey, Root: "recovery/test/", AuthURL: f.srv.URL, HTTP: f.srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func writeB2Error(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "code": code, "message": code})
}

func (f *fakeB2) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/b2api/v4/b2_authorize_account":
		user, pass, ok := r.BasicAuth()
		if !ok || user != fakeKeyID || pass != fakeKey {
			writeB2Error(w, 401, "unauthorized")
			return
		}
		f.authCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accountId":          "acct",
			"authorizationToken": "api-token",
			"apiInfo": map[string]any{"storageApi": map[string]any{
				"apiUrl": f.srv.URL, "downloadUrl": f.srv.URL,
				"allowed": map[string]any{"buckets": f.buckets, "capabilities": f.capabilities, "namePrefix": f.namePrefix},
			}},
		})
	case r.URL.Path == "/b2api/v4/b2_get_upload_url":
		if r.Header.Get("Authorization") != "api-token" {
			writeB2Error(w, 401, "bad_auth_token")
			return
		}
		if f.expireAPIToken {
			f.expireAPIToken = false
			writeB2Error(w, 401, "expired_auth_token")
			return
		}
		var body struct {
			BucketID string `json:"bucketId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.BucketID != "bucket-1" {
			writeB2Error(w, 400, "bad_bucket_id")
			return
		}
		f.uploadURLs++
		_ = json.NewEncoder(w).Encode(map[string]any{"uploadUrl": f.srv.URL + "/upload/bucket-1", "authorizationToken": "upload-token"})
	case r.URL.Path == "/upload/bucket-1":
		if r.Header.Get("Authorization") != "upload-token" {
			writeB2Error(w, 401, "bad_auth_token")
			return
		}
		if f.expireUploadToken {
			f.expireUploadToken = false
			writeB2Error(w, 401, "expired_auth_token")
			return
		}
		if f.uploadUnavailable {
			f.uploadUnavailable = false
			writeB2Error(w, 503, "service_unavailable")
			return
		}
		name, err := url.PathUnescape(r.Header.Get("X-Bz-File-Name"))
		if err != nil || r.ContentLength < 0 {
			writeB2Error(w, 400, "bad_request")
			return
		}
		data, _ := io.ReadAll(r.Body)
		sum := sha1.Sum(data)
		if r.Header.Get("X-Bz-Content-Sha1") != hex.EncodeToString(sum[:]) {
			writeB2Error(w, 400, "bad_request")
			return
		}
		f.files[name] = data
		reported := hex.EncodeToString(sum[:])
		if f.wrongSha1 {
			f.wrongSha1 = false
			reported = strings.Repeat("0", 40)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"fileName": name, "contentSha1": reported, "action": "upload"})
	case strings.HasPrefix(r.URL.Path, "/file/"+fakeBucket+"/"):
		if r.Header.Get("Authorization") != "api-token" {
			writeB2Error(w, 401, "bad_auth_token")
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/file/"+fakeBucket+"/")
		data, ok := f.files[name]
		if !ok {
			writeB2Error(w, 404, "not_found")
			return
		}
		_, _ = w.Write(data)
	case r.URL.Path == "/b2api/v4/b2_list_file_names":
		if r.Header.Get("Authorization") != "api-token" {
			writeB2Error(w, 401, "bad_auth_token")
			return
		}
		var body struct {
			BucketID      string `json:"bucketId"`
			Prefix        string `json:"prefix"`
			StartFileName string `json:"startFileName"`
			MaxFileCount  int    `json:"maxFileCount"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var names []string
		for name := range f.files {
			if strings.HasPrefix(name, body.Prefix) && name >= body.StartFileName {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		var next any
		if body.MaxFileCount > 0 && len(names) > body.MaxFileCount {
			next = names[body.MaxFileCount]
			names = names[:body.MaxFileCount]
		}
		files := []map[string]string{}
		for _, n := range names {
			files = append(files, map[string]string{"fileName": n, "action": "upload"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"files": files, "nextFileName": next})
	default:
		writeB2Error(w, 404, "not_found")
	}
}

func TestB2PutGetListRoundTrip(t *testing.T) {
	ctx := context.Background()
	f := newFakeB2(t)
	j := f.journal(t)
	j.listPageSize = 2 // exercise pagination
	if err := j.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	for _, n := range []string{"recovery/test/op/a/0000000001-x.prepared", "recovery/test/op/a/0000000001-x.outcome", "recovery/test/blob/abc", "recovery/test/op/b/0000000000-y.prepared"} {
		if err := j.Put(ctx, n, []byte("data:"+n)); err != nil {
			t.Fatalf("Put %s: %v", n, err)
		}
	}
	got, err := j.Get(ctx, "recovery/test/blob/abc")
	if err != nil || string(got) != "data:recovery/test/blob/abc" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if _, err := j.Get(ctx, "recovery/test/blob/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: err = %v, want ErrNotFound", err)
	}
	names, err := j.List(ctx, "recovery/test/op/")
	if err != nil {
		t.Fatal(err)
	}
	want := "recovery/test/op/a/0000000001-x.outcome,recovery/test/op/a/0000000001-x.prepared,recovery/test/op/b/0000000000-y.prepared"
	if strings.Join(names, ",") != want {
		t.Fatalf("List = %v, want %s", names, want)
	}
	if f.authCalls != 1 {
		t.Errorf("authorized %d times, want 1 (token reused)", f.authCalls)
	}
}

// TestB2KeyMustBeNarrowlyScoped enforces the approved plan: a key limited to
// one bucket and the recovery/ prefix, able to list/read/write, never delete.
func TestB2KeyMustBeNarrowlyScoped(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fakeB2)
		want   string
	}{
		{"delete capability", func(f *fakeB2) { f.capabilities = append(f.capabilities, "deleteFiles") }, "delete"},
		{"no write capability", func(f *fakeB2) { f.capabilities = []string{"listFiles", "readFiles"} }, "writeFiles"},
		{"no prefix restriction", func(f *fakeB2) { f.namePrefix = nil }, "prefix"},
		{"other prefix", func(f *fakeB2) { f.namePrefix = "database/" }, "prefix"},
		{"all buckets", func(f *fakeB2) { f.buckets = nil }, "one bucket"},
		{"two buckets", func(f *fakeB2) {
			f.buckets = append(f.buckets, map[string]any{"id": "bucket-2", "name": "other"})
		}, "one bucket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeB2(t)
			tc.mutate(f)
			err := f.journal(t).Ping(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Ping err = %v, want mention of %q", err, tc.want)
			}
			if strings.Contains(err.Error(), fakeKey) {
				t.Fatalf("error echoes the application key: %v", err)
			}
		})
	}
}

func TestB2UploadAcknowledgmentAndRetries(t *testing.T) {
	ctx := context.Background()
	t.Run("checksum mismatch is not an acknowledgment", func(t *testing.T) {
		f := newFakeB2(t)
		f.wrongSha1 = true
		if err := f.journal(t).Put(ctx, "recovery/test/x", []byte("v")); err == nil || !strings.Contains(err.Error(), "checksum") {
			t.Fatalf("Put err = %v, want checksum mismatch", err)
		}
	})
	t.Run("expired upload token gets a new upload URL", func(t *testing.T) {
		f := newFakeB2(t)
		f.expireUploadToken = true
		if err := f.journal(t).Put(ctx, "recovery/test/x", []byte("v")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if f.uploadURLs != 2 {
			t.Errorf("upload URLs fetched %d times, want 2", f.uploadURLs)
		}
	})
	t.Run("expired API token re-authorizes once", func(t *testing.T) {
		f := newFakeB2(t)
		f.expireAPIToken = true
		if err := f.journal(t).Put(ctx, "recovery/test/x", []byte("v")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if f.authCalls != 2 {
			t.Errorf("authorized %d times, want 2", f.authCalls)
		}
	})
	t.Run("unavailable upload is an error for the caller to retry", func(t *testing.T) {
		f := newFakeB2(t)
		f.uploadUnavailable = true
		j := f.journal(t)
		if err := j.Put(ctx, "recovery/test/x", []byte("v")); err == nil {
			t.Fatal("Put succeeded although B2 answered 503")
		}
		if err := j.Put(ctx, "recovery/test/x", []byte("v")); err != nil {
			t.Fatalf("Put after the outage: %v", err)
		}
		if f.uploadURLs != 2 {
			t.Errorf("upload URLs fetched %d times, want 2 (the 503 URL is discarded)", f.uploadURLs)
		}
	})
}

func TestB2RefusesNamesOutsideItsRoot(t *testing.T) {
	ctx := context.Background()
	j := newFakeB2(t).journal(t)
	for _, name := range []string{"database/x", "recovery/other-env/x", "recovery/test/../x", "recovery/test/Upper", "recovery/test/a b"} {
		if err := j.Put(ctx, name, []byte("v")); err == nil {
			t.Errorf("Put(%q) accepted", name)
		}
		if _, err := j.Get(ctx, name); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) err = %v, want refusal", name, err)
		}
	}
}

func TestNewB2RequiresCredentialsAndRoot(t *testing.T) {
	for _, cfg := range []B2Config{
		{Key: fakeKey, Root: "recovery/test/"},
		{KeyID: fakeKeyID, Root: "recovery/test/"},
		{KeyID: fakeKeyID, Key: fakeKey},
		{KeyID: fakeKeyID, Key: fakeKey, Root: "recovery/test"},
	} {
		if _, err := NewB2(cfg); err == nil {
			t.Errorf("NewB2(%+v) accepted", B2Config{KeyID: cfg.KeyID, Root: cfg.Root})
		}
	}
}
