package recovery

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// B2Config configures the Backblaze B2 journal (native API v4).
type B2Config struct {
	KeyID string
	// Key is the application key; it is never logged or echoed.
	Key string
	// Root is the journal's name prefix, for example "recovery/staging/". It
	// must end with "/" and lie inside the key's name-prefix restriction.
	Root    string
	AuthURL string // default https://api.backblazeb2.com
	HTTP    *http.Client
}

// B2 is a Journal on Backblaze B2. It requires an application key restricted
// to exactly one bucket and the name prefix "recovery/" (or exactly its
// root), with listFiles, readFiles and writeFiles and otherwise only
// read-only capabilities, as the approved recovery plan specifies (no delete,
// no bucket or retention changes); any other key is refused before use.
type B2 struct {
	cfg          B2Config
	listPageSize int
	authSem      chan struct{} // serializes authorization; acquiring honors ctx

	mu      sync.Mutex
	auth    *b2Auth
	uploads []b2Upload // idle upload targets; B2 allows one upload at a time per target
}

type b2Auth struct {
	token, apiURL, downloadURL string
	bucketID, bucketName       string
}

type b2Upload struct {
	url, token string
	auth       *b2Auth // authorization the target was obtained under
}

// b2Error is a B2 error response; Code is B2's stable error code.
type b2Error struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Op     string `json:"-"`
}

func (e *b2Error) Error() string { return fmt.Sprintf("b2 %s: HTTP %d %s", e.Op, e.Status, e.Code) }

func isExpired(err error) bool {
	var be *b2Error
	return errors.As(err, &be) && be.Status == http.StatusUnauthorized && (be.Code == "expired_auth_token" || be.Code == "bad_auth_token")
}

var objectName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$`)

// NewB2 validates cfg; the key's scope is checked on first use.
func NewB2(cfg B2Config) (*B2, error) {
	switch {
	case cfg.KeyID == "" || cfg.Key == "":
		return nil, errors.New("recovery: B2 key id and application key are required")
	case !strings.HasSuffix(cfg.Root, "/") || !objectName.MatchString(strings.TrimSuffix(cfg.Root, "/")):
		return nil, errors.New("recovery: B2 journal root must be a lower-case prefix ending in /")
	}
	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://api.backblazeb2.com"
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 2 * time.Minute}
	}
	return &B2{cfg: cfg, listPageSize: 1000, authSem: make(chan struct{}, 1)}, nil
}

var (
	requiredCaps = []string{"listFiles", "readFiles", "writeFiles"}
	// Read-only capabilities a restricted key may also carry.
	readOnlyCaps = []string{"listBuckets", "listAllBucketNames", "readBuckets", "readBucketEncryption",
		"readBucketRetentions", "readFileRetentions", "readFileLegalHolds", "readBucketNotifications",
		"readBucketReplications", "readBucketLogging"}
)

func (b *B2) checkName(name string) error {
	if !strings.HasPrefix(name, b.cfg.Root) || !objectName.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("recovery: object name %q is outside the journal root %q", name, b.cfg.Root)
	}
	return nil
}

func escapeName(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// do sends req and decodes a JSON response into out (nil to skip). Non-2xx
// responses become *b2Error.
func (b *B2) do(req *http.Request, op string, out any) ([]byte, error) {
	resp, err := b.cfg.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("b2 %s: %w", op, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("b2 %s: read: %w", op, err)
	}
	if resp.StatusCode/100 != 2 {
		be := &b2Error{Status: resp.StatusCode, Op: op}
		_ = json.Unmarshal(body, be)
		be.Status = resp.StatusCode
		return nil, be
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return nil, fmt.Errorf("b2 %s: decode: %w", op, err)
		}
	}
	return body, nil
}

// authorize returns a cached authorization, verifying the key's scope. A
// fresh authorization also discards idle upload targets.
func (b *B2) authorize(ctx context.Context, fresh bool) (*b2Auth, error) {
	b.mu.Lock()
	cached := b.auth
	b.mu.Unlock()
	if cached != nil && !fresh {
		return cached, nil
	}
	select {
	case b.authSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-b.authSem }()
	b.mu.Lock()
	if b.auth != nil && b.auth != cached {
		// Another caller authorized while this one waited.
		auth := b.auth
		b.mu.Unlock()
		return auth, nil
	}
	b.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(b.cfg.AuthURL, "/")+"/b2api/v4/b2_authorize_account", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(b.cfg.KeyID, b.cfg.Key)
	var resp struct {
		AuthorizationToken string `json:"authorizationToken"`
		APIInfo            struct {
			StorageAPI struct {
				APIURL      string `json:"apiUrl"`
				DownloadURL string `json:"downloadUrl"`
				Allowed     struct {
					Buckets []struct {
						ID   string  `json:"id"`
						Name *string `json:"name"`
					} `json:"buckets"`
					Capabilities []string `json:"capabilities"`
					NamePrefix   *string  `json:"namePrefix"`
				} `json:"allowed"`
			} `json:"storageApi"`
		} `json:"apiInfo"`
	}
	if _, err := b.do(req, "authorize", &resp); err != nil {
		return nil, err
	}
	s := resp.APIInfo.StorageAPI
	if err := b.checkScope(s.Allowed.Capabilities, s.Allowed.NamePrefix); err != nil {
		return nil, err
	}
	switch {
	case len(s.Allowed.Buckets) != 1 || s.Allowed.Buckets[0].Name == nil || s.Allowed.Buckets[0].ID == "":
		return nil, errors.New("recovery: B2 key must be restricted to one bucket")
	case resp.AuthorizationToken == "" || s.APIURL == "" || s.DownloadURL == "":
		return nil, errors.New("recovery: B2 authorization response is incomplete")
	}
	auth := &b2Auth{
		token: resp.AuthorizationToken, apiURL: strings.TrimRight(s.APIURL, "/"), downloadURL: strings.TrimRight(s.DownloadURL, "/"),
		bucketID: s.Allowed.Buckets[0].ID, bucketName: *s.Allowed.Buckets[0].Name,
	}
	b.mu.Lock()
	b.auth, b.uploads = auth, nil
	b.mu.Unlock()
	return auth, nil
}

// checkScope enforces the approved key: the three journal capabilities plus
// read-only ones, and the "recovery/" prefix (or exactly the journal root).
func (b *B2) checkScope(caps []string, prefix *string) error {
	for _, c := range requiredCaps {
		if !slices.Contains(caps, c) {
			return errors.New("recovery: B2 key needs listFiles, readFiles and writeFiles")
		}
	}
	for _, c := range caps {
		switch {
		case c == "deleteFiles":
			return errors.New("recovery: B2 key can delete files; the recovery journal requires a key without delete")
		case !slices.Contains(requiredCaps, c) && !slices.Contains(readOnlyCaps, c):
			return fmt.Errorf("recovery: B2 key has capability %q; only list/read/write files and read-only capabilities are allowed", c)
		}
	}
	if prefix == nil || (*prefix != "recovery/" && *prefix != b.cfg.Root) {
		return fmt.Errorf(`recovery: B2 key must be restricted to the name prefix "recovery/" or %q`, b.cfg.Root)
	}
	return nil
}

// api calls a JSON API operation, re-authorizing once on an expired token.
func (b *B2) api(ctx context.Context, op string, in, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		auth, err := b.authorize(ctx, attempt > 0)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.apiURL+"/b2api/v4/"+op, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", auth.token)
		req.Header.Set("Content-Type", "application/json")
		_, err = b.do(req, op, out)
		if err != nil && isExpired(err) && attempt == 0 {
			continue
		}
		return err
	}
}

// Ping implements Journal: authorizes (checking the key's scope) and lists
// one name under the root.
func (b *B2) Ping(ctx context.Context) error {
	_, err := b.list(ctx, b.cfg.Root, 1)
	return err
}

// Put implements Journal. It returns only after B2 acknowledges the upload
// with the SHA-1 of exactly these bytes. Each upload holds its own upload
// target exclusively; a target that failed is discarded.
func (b *B2) Put(ctx context.Context, name string, data []byte) error {
	if err := b.checkName(name); err != nil {
		return err
	}
	sum := sha1.Sum(data)
	want := hex.EncodeToString(sum[:])
	for attempt := 0; ; attempt++ {
		up, err := b.takeUpload(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, up.url, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.ContentLength = int64(len(data))
		req.Header.Set("Authorization", up.token)
		req.Header.Set("X-Bz-File-Name", escapeName(name))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-Bz-Content-Sha1", want)
		var resp struct {
			FileName    string `json:"fileName"`
			ContentSha1 string `json:"contentSha1"`
		}
		_, err = b.do(req, "upload", &resp)
		if err != nil {
			// B2 asks for a new upload URL after any upload error; this
			// target is not returned to the pool. An expired token likely
			// means the idle ones are expired too.
			if isExpired(err) && attempt == 0 {
				b.mu.Lock()
				b.uploads = nil
				b.mu.Unlock()
				continue
			}
			return err
		}
		b.returnUpload(up)
		if resp.FileName != name || resp.ContentSha1 != want {
			return fmt.Errorf("b2 upload %s: acknowledgment checksum or name does not match the sent bytes", name)
		}
		return nil
	}
}

// takeUpload removes an idle upload target from the pool or gets a new one.
func (b *B2) takeUpload(ctx context.Context) (b2Upload, error) {
	b.mu.Lock()
	if n := len(b.uploads); n > 0 {
		up := b.uploads[n-1]
		b.uploads = b.uploads[:n-1]
		b.mu.Unlock()
		return up, nil
	}
	b.mu.Unlock()
	auth, err := b.authorize(ctx, false)
	if err != nil {
		return b2Upload{}, err
	}
	var resp struct {
		UploadURL          string `json:"uploadUrl"`
		AuthorizationToken string `json:"authorizationToken"`
	}
	if err := b.api(ctx, "b2_get_upload_url", map[string]string{"bucketId": auth.bucketID}, &resp); err != nil {
		return b2Upload{}, err
	}
	return b2Upload{url: resp.UploadURL, token: resp.AuthorizationToken, auth: auth}, nil
}

// returnUpload puts a target back unless a newer authorization replaced it.
func (b *B2) returnUpload(up b2Upload) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if up.auth == b.auth && len(b.uploads) < 8 {
		b.uploads = append(b.uploads, up)
	}
}

// Get implements Journal.
func (b *B2) Get(ctx context.Context, name string) ([]byte, error) {
	if err := b.checkName(name); err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		auth, err := b.authorize(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			auth.downloadURL+"/file/"+url.PathEscape(auth.bucketName)+"/"+escapeName(name), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", auth.token)
		data, err := b.do(req, "download", nil)
		var be *b2Error
		switch {
		case err != nil && isExpired(err) && attempt == 0:
			continue
		case errors.As(err, &be) && be.Status == http.StatusNotFound:
			return nil, ErrNotFound
		}
		return data, err
	}
}

// List implements Journal.
func (b *B2) List(ctx context.Context, prefix string) ([]string, error) {
	if !strings.HasPrefix(prefix, b.cfg.Root) {
		return nil, fmt.Errorf("recovery: list prefix %q is outside the journal root %q", prefix, b.cfg.Root)
	}
	return b.list(ctx, prefix, 0)
}

func (b *B2) list(ctx context.Context, prefix string, limit int) ([]string, error) {
	auth, err := b.authorize(ctx, false)
	if err != nil {
		return nil, err
	}
	var names []string
	start := ""
	for {
		page := b.listPageSize
		if limit > 0 {
			page = limit
		}
		var resp struct {
			Files []struct {
				FileName string `json:"fileName"`
				Action   string `json:"action"`
			} `json:"files"`
			NextFileName *string `json:"nextFileName"`
		}
		in := map[string]any{"bucketId": auth.bucketID, "prefix": prefix, "maxFileCount": page}
		if start != "" {
			in["startFileName"] = start
		}
		if err := b.api(ctx, "b2_list_file_names", in, &resp); err != nil {
			return nil, err
		}
		for _, f := range resp.Files {
			if f.Action == "" || f.Action == "upload" {
				names = append(names, f.FileName)
			}
		}
		if resp.NextFileName == nil || *resp.NextFileName == "" || (limit > 0 && len(names) >= limit) {
			return names, nil
		}
		start = *resp.NextFileName
	}
}
