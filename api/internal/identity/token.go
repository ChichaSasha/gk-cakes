// Package identity verifies the managed identity provider's access tokens
// (Supabase, contracts r6.5 K00) and resolves the local account and staff
// capabilities of a verified subject. Authorization never reads provider
// metadata: roles exist only as locally granted capabilities.
package identity

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Audience is the only accepted token audience: Supabase's signed-in users.
const Audience = "authenticated"

// MaxTokenBytes bounds an access token before any parsing.
const MaxTokenBytes = 8 << 10

const (
	maxJWKSBytes = 64 << 10
	// fetchTimeout bounds one key-set fetch; it runs detached from the
	// request that triggered it, so a cancelled request cannot abort it.
	fetchTimeout = 5 * time.Second
)

var (
	// ErrUnauthenticated: the token is missing, malformed, not signed by a
	// current provider key, expired, or not a signed-in user's token.
	ErrUnauthenticated = errors.New("identity: access token not accepted")
	// ErrSessionEnded: the token is valid but its provider session no longer
	// exists (signed out, or ended by the provider).
	ErrSessionEnded = errors.New("identity: provider session ended")
	// ErrUnavailable: the provider's keys or session check could not be
	// reached; the request fails closed.
	ErrUnavailable = errors.New("identity: identity provider unavailable")
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	kidPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	b64         = base64.RawURLEncoding.Strict()
)

// Claims are the verified facts of an access token.
type Claims struct {
	Subject   string // provider user ID (UUID)
	SessionID string // provider session ID (UUID)
	ExpiresAt time.Time
}

// Verifier checks ES256 access tokens against the provider's JWKS, fetched
// from the fixed configured issuer.
//
// Keys are cached. A token whose key is unknown (a rotation) or whose cached
// set is older than MaxKeyAge waits for a refresh; refreshes start at most
// once per RefreshInterval (RetryInterval while no key set was ever
// fetched), run one at a time in the background with their own timeout, and
// never block a token whose key is cached and fresh. If a refresh fails, the
// cached set stays usable until HardKeyAge, then tokens fail closed.
type Verifier struct {
	// Issuer is the exact iss claim, e.g. https://<ref>.supabase.co/auth/v1;
	// the key set is read from Issuer + "/.well-known/jwks.json".
	Issuer string
	// HTTP fetches the key set; it should not follow redirects (NoRedirects).
	HTTP *http.Client
	// Leeway tolerates clock skew on exp, nbf and iat (default 30s).
	Leeway time.Duration
	// RefreshInterval is the minimum time between key-set fetches (default 30s).
	RefreshInterval time.Duration
	// RetryInterval replaces RefreshInterval while no key set was ever
	// fetched (default 5s).
	RetryInterval time.Duration
	// MaxKeyAge refreshes the key set when older (default 10m), so a key
	// withdrawn by the provider stops being accepted.
	MaxKeyAge time.Duration
	// HardKeyAge is how long a set that cannot be refreshed stays trusted
	// (default 1h); after that, tokens fail closed.
	HardKeyAge time.Duration
	// Now returns the current time (default time.Now).
	Now func() time.Time

	mu          sync.Mutex
	keys        map[string]*ecdsa.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
	lastErr     error
	inflight    chan struct{} // closed when the running refresh ends
}

// NoRedirects is an http.Client CheckRedirect policy for provider calls: the
// configured issuer is fixed, so a redirect is answered as an error status.
func NoRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func (v *Verifier) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Verify checks the token's signature, issuer, audience, time window,
// subject, session and role. Any failure is ErrUnauthenticated, except an
// unusable key set, which is ErrUnavailable. Errors never contain the token.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	if token == "" || len(token) > MaxTokenBytes {
		return Claims{}, fmt.Errorf("%w: missing or oversized", ErrUnauthenticated)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: not a signed token", ErrUnauthenticated)
	}
	headerJSON, err1 := b64.DecodeString(parts[0])
	payload, err2 := b64.DecodeString(parts[1])
	sig, err3 := b64.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return Claims{}, fmt.Errorf("%w: malformed encoding", ErrUnauthenticated)
	}
	header, err := object(headerJSON)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: malformed header: %v", ErrUnauthenticated, err)
	}
	alg, okAlg := str(header, "alg")
	kid, okKid := str(header, "kid")
	typ, okTyp := str(header, "typ")
	_, hasTyp := header["typ"]
	_, hasCrit := header["crit"]
	switch {
	case !okAlg || alg != "ES256":
		return Claims{}, fmt.Errorf("%w: algorithm not accepted", ErrUnauthenticated)
	case !okKid || !kidPattern.MatchString(kid):
		return Claims{}, fmt.Errorf("%w: missing key ID", ErrUnauthenticated)
	case hasTyp && (!okTyp || !strings.EqualFold(typ, "JWT")):
		return Claims{}, fmt.Errorf("%w: type not accepted", ErrUnauthenticated)
	case hasCrit:
		return Claims{}, fmt.Errorf("%w: critical header extensions not supported", ErrUnauthenticated)
	case len(sig) != 64:
		return Claims{}, fmt.Errorf("%w: malformed signature", ErrUnauthenticated)
	}
	key, err := v.key(ctx, kid)
	if err != nil {
		return Claims{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(key, digest[:], r, s) {
		return Claims{}, fmt.Errorf("%w: bad signature", ErrUnauthenticated)
	}
	return v.claims(payload)
}

func (v *Verifier) claims(payload []byte) (Claims, error) {
	c, err := object(payload)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: malformed claims: %v", ErrUnauthenticated, err)
	}
	now := v.now()
	leeway := orDefault(v.Leeway, 30*time.Second)
	iss, okIss := str(c, "iss")
	sub, okSub := str(c, "sub")
	sid, okSid := str(c, "session_id")
	role, okRole := str(c, "role")
	exp, okExp := unix(c, "exp")
	nbf, okNbf := unix(c, "nbf")
	iat, okIat := unix(c, "iat")
	_, hasNbf := c["nbf"]
	_, hasIat := c["iat"]
	anonymous, hasAnon := c["is_anonymous"]
	switch {
	case !okIss || iss != v.Issuer:
		return Claims{}, fmt.Errorf("%w: wrong issuer", ErrUnauthenticated)
	case !hasAudience(c["aud"], Audience):
		return Claims{}, fmt.Errorf("%w: wrong audience", ErrUnauthenticated)
	case !okExp:
		return Claims{}, fmt.Errorf("%w: no valid expiry", ErrUnauthenticated)
	case !now.Before(exp.Add(leeway)):
		return Claims{}, fmt.Errorf("%w: expired", ErrUnauthenticated)
	case hasNbf && (!okNbf || nbf.After(now.Add(leeway))):
		return Claims{}, fmt.Errorf("%w: not valid yet", ErrUnauthenticated)
	case hasIat && (!okIat || iat.After(now.Add(leeway))):
		return Claims{}, fmt.Errorf("%w: issued in the future", ErrUnauthenticated)
	case !okSub || !uuidPattern.MatchString(sub):
		return Claims{}, fmt.Errorf("%w: no user subject", ErrUnauthenticated)
	case !okSid || !uuidPattern.MatchString(sid):
		return Claims{}, fmt.Errorf("%w: no session", ErrUnauthenticated)
	case !okRole || role != Audience:
		return Claims{}, fmt.Errorf("%w: not a signed-in user's token", ErrUnauthenticated)
	case hasAnon && string(anonymous) != "false":
		return Claims{}, fmt.Errorf("%w: anonymous session", ErrUnauthenticated)
	}
	return Claims{Subject: sub, SessionID: sid, ExpiresAt: exp}, nil
}

// object decodes exactly one JSON object into its members by exact name,
// refusing duplicate names and trailing data (RFC 7515 §4, RFC 7519 §4).
func object(b []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	members := map[string]json.RawMessage{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		name, ok := t.(string)
		if !ok {
			return nil, errors.New("bad member name")
		}
		if _, dup := members[name]; dup {
			return nil, fmt.Errorf("duplicate member %q", name)
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, err
		}
		members[name] = raw
	}
	if _, err := d.Token(); err != nil { // the closing brace
		return nil, err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data")
	}
	return members, nil
}

// str returns a member that is a JSON string.
func str(m map[string]json.RawMessage, name string) (string, bool) {
	raw, ok := m[name]
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// unix returns a member that is a JSON number of seconds since the epoch,
// before year 5138.
func unix(m map[string]json.RawMessage, name string) (time.Time, bool) {
	raw, ok := m[name]
	if !ok || len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
		return time.Time{}, false
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || f < 0 || f > 1e11 {
		return time.Time{}, false
	}
	sec := int64(f)
	return time.Unix(sec, int64((f-float64(sec))*1e9)), true
}

func hasAudience(raw json.RawMessage, want string) bool {
	var one string
	if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &one) == nil {
		return one == want
	}
	var many []string
	if len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == want {
				return true
			}
		}
	}
	return false
}

// key returns the public key for kid. See Verifier for the refresh rules.
func (v *Verifier) key(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	maxAge := orDefault(v.MaxKeyAge, 10*time.Minute)
	for waited := false; ; waited = true {
		v.mu.Lock()
		now := v.now()
		k, known := v.keys[kid]
		if known && now.Sub(v.fetchedAt) <= maxAge {
			v.mu.Unlock()
			return k, nil
		}
		if !waited && v.inflight == nil && v.due(now) {
			v.lastAttempt = now
			v.inflight = make(chan struct{})
			go v.refresh(v.inflight)
		}
		wait := v.inflight
		v.mu.Unlock()
		if waited || wait == nil {
			break
		}
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: waiting for signing keys: %v", ErrUnavailable, ctx.Err())
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	k, known := v.keys[kid]
	switch {
	case v.keys == nil:
		return nil, fmt.Errorf("%w: signing keys not available: %v", ErrUnavailable, v.lastErr)
	case known && v.now().Sub(v.fetchedAt) <= orDefault(v.HardKeyAge, time.Hour):
		return k, nil // stale, and the refresh failed or is not due yet
	case known:
		return nil, fmt.Errorf("%w: signing keys too old to trust: %v", ErrUnavailable, v.lastErr)
	}
	return nil, fmt.Errorf("%w: unknown signing key", ErrUnauthenticated)
}

// due reports whether a refresh may start now (v.mu held).
func (v *Verifier) due(now time.Time) bool {
	interval := orDefault(v.RefreshInterval, 30*time.Second)
	if v.keys == nil {
		interval = orDefault(v.RetryInterval, 5*time.Second)
	}
	return v.lastAttempt.IsZero() || now.Sub(v.lastAttempt) >= interval
}

// refresh fetches the key set detached from any request and publishes it;
// a failed fetch keeps the cached set.
func (v *Verifier) refresh(done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	keys, err := v.fetch(ctx)
	cancel()
	v.mu.Lock()
	if err == nil {
		v.keys, v.fetchedAt = keys, v.now()
	}
	v.lastErr = err
	v.inflight = nil
	v.mu.Unlock()
	close(done)
}

type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// fetch reads the JWKS and keeps only ES256 signing keys on P-256 whose
// point is on the curve.
func (v *Verifier) fetch(ctx context.Context) (map[string]*ecdsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.Issuer+"/.well-known/jwks.json", nil)
	if err != nil {
		return nil, errors.New("jwks: bad issuer URL")
	}
	client := v.HTTP
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout, CheckRedirect: NoRedirects}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil || len(body) > maxJWKSBytes {
		return nil, errors.New("jwks: unreadable or oversized response")
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, errors.New("jwks: malformed response")
	}
	keys := map[string]*ecdsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "EC" || k.Crv != "P-256" || (k.Alg != "" && k.Alg != "ES256") ||
			(k.Use != "" && k.Use != "sig") || !kidPattern.MatchString(k.Kid) {
			continue
		}
		x, errX := b64.DecodeString(k.X)
		y, errY := b64.DecodeString(k.Y)
		if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
			continue
		}
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("jwks: no ES256 signing key")
	}
	return keys, nil
}
