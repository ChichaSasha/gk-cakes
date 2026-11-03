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
// to exactly one bucket and a name prefix, with list/read/write capabilities
// and without deleteFiles, as the approved recovery plan specifies; any other
// key is refused before use.
type B2 struct {
	cfg          B2Config
	listPageSize int

	mu     sync.Mutex
	auth   *b2Auth
	upload *b2Upload
}

type b2Auth struct {
	token, apiURL, downloadURL string
	bucketID, bucketName       string
}

type b2Upload struct{ url, token string }

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
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return &B2{cfg: cfg, listPageSize: 1000}, nil
}

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

// authorize returns a cached authorization, verifying the key's scope.
func (b *B2) authorize(ctx context.Context, fresh bool) (*b2Auth, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.auth != nil && !fresh {
		return b.auth, nil
	}
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
	caps := s.Allowed.Capabilities
	switch {
	case len(s.Allowed.Buckets) != 1 || s.Allowed.Buckets[0].Name == nil || s.Allowed.Buckets[0].ID == "":
		return nil, errors.New("recovery: B2 key must be restricted to one bucket")
	case slices.Contains(caps, "deleteFiles"):
		return nil, errors.New("recovery: B2 key can delete files; the recovery journal requires a key without delete")
	case !slices.Contains(caps, "writeFiles") || !slices.Contains(caps, "readFiles") || !slices.Contains(caps, "listFiles"):
		return nil, errors.New("recovery: B2 key needs listFiles, readFiles and writeFiles")
	case s.Allowed.NamePrefix == nil || *s.Allowed.NamePrefix == "" || !strings.HasPrefix(b.cfg.Root, *s.Allowed.NamePrefix):
		return nil, fmt.Errorf("recovery: B2 key must be restricted to a name prefix containing %q", b.cfg.Root)
	case resp.AuthorizationToken == "" || s.APIURL == "" || s.DownloadURL == "":
		return nil, errors.New("recovery: B2 authorization response is incomplete")
	}
	b.auth = &b2Auth{
		token: resp.AuthorizationToken, apiURL: strings.TrimRight(s.APIURL, "/"), downloadURL: strings.TrimRight(s.DownloadURL, "/"),
		bucketID: s.Allowed.Buckets[0].ID, bucketName: *s.Allowed.Buckets[0].Name,
	}
	b.upload = nil
	return b.auth, nil
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
// with the SHA-1 of exactly these bytes.
func (b *B2) Put(ctx context.Context, name string, data []byte) error {
	if err := b.checkName(name); err != nil {
		return err
	}
	sum := sha1.Sum(data)
	want := hex.EncodeToString(sum[:])
	for attempt := 0; ; attempt++ {
		up, err := b.uploadTarget(ctx)
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
			// Upload URLs are single-use after an error; fetch a new one.
			b.mu.Lock()
			b.upload = nil
			b.mu.Unlock()
			if isExpired(err) && attempt == 0 {
				continue
			}
			return err
		}
		if resp.FileName != name || resp.ContentSha1 != want {
			return fmt.Errorf("b2 upload %s: acknowledgment checksum or name does not match the sent bytes", name)
		}
		return nil
	}
}

func (b *B2) uploadTarget(ctx context.Context) (b2Upload, error) {
	b.mu.Lock()
	if b.upload != nil {
		up := *b.upload
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
	up := b2Upload{url: resp.UploadURL, token: resp.AuthorizationToken}
	b.mu.Lock()
	b.upload = &up
	b.mu.Unlock()
	return up, nil
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
