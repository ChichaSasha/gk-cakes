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
	"strings"
	"sync"
	"time"
)

// Audience is the only accepted token audience: Supabase's signed-in users.
const Audience = "authenticated"

// MaxTokenBytes bounds an access token before any parsing.
const MaxTokenBytes = 8 << 10

const maxJWKSBytes = 64 << 10

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
)

// Claims are the verified facts of an access token.
type Claims struct {
	Subject   string // provider user ID (UUID)
	SessionID string // provider session ID (UUID)
	ExpiresAt time.Time
}

// Verifier checks ES256 access tokens against the provider's JWKS, fetched
// from the fixed configured issuer. Keys are cached; an unknown key ID
// refetches the set (a key rotation), at most once per RefreshInterval.
type Verifier struct {
	// Issuer is the exact iss claim, e.g. https://<ref>.supabase.co/auth/v1;
	// the key set is read from Issuer + "/.well-known/jwks.json".
	Issuer string
	HTTP   *http.Client
	// Leeway tolerates clock skew on exp, nbf and iat (default 30s).
	Leeway time.Duration
	// RefreshInterval is the minimum time between key-set fetches (default 30s).
	RefreshInterval time.Duration
	// MaxKeyAge refetches the key set when older (default 10m), so a key
	// withdrawn by the provider stops being accepted.
	MaxKeyAge time.Duration
	// Now returns the current time (default time.Now).
	Now func() time.Time

	mu          sync.Mutex
	keys        map[string]*ecdsa.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
}

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

type tokenHeader struct {
	Alg  string          `json:"alg"`
	Kid  string          `json:"kid"`
	Typ  string          `json:"typ"`
	Crit json.RawMessage `json:"crit"`
}

type tokenClaims struct {
	Iss         *string         `json:"iss"`
	Aud         json.RawMessage `json:"aud"`
	Sub         *string         `json:"sub"`
	Exp         *json.Number    `json:"exp"`
	Nbf         *json.Number    `json:"nbf"`
	Iat         *json.Number    `json:"iat"`
	Role        *string         `json:"role"`
	SessionID   *string         `json:"session_id"`
	IsAnonymous *bool           `json:"is_anonymous"`
}

// Verify checks the token's signature, issuer, audience, time window,
// subject, session and role. Any failure is ErrUnauthenticated, except an
// unreachable key set with no cached key, which is ErrUnavailable. Errors
// never contain the token.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	if token == "" || len(token) > MaxTokenBytes {
		return Claims{}, fmt.Errorf("%w: missing or oversized", ErrUnauthenticated)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: not a signed token", ErrUnauthenticated)
	}
	headerJSON, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	payload, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	sig, err3 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return Claims{}, fmt.Errorf("%w: malformed encoding", ErrUnauthenticated)
	}
	var h tokenHeader
	if err := strictJSON(headerJSON, &h); err != nil {
		return Claims{}, fmt.Errorf("%w: malformed header", ErrUnauthenticated)
	}
	switch {
	case h.Alg != "ES256":
		return Claims{}, fmt.Errorf("%w: algorithm %q not accepted", ErrUnauthenticated, h.Alg)
	case !kidPattern.MatchString(h.Kid):
		return Claims{}, fmt.Errorf("%w: missing key ID", ErrUnauthenticated)
	case h.Typ != "" && !strings.EqualFold(h.Typ, "JWT"):
		return Claims{}, fmt.Errorf("%w: type %q not accepted", ErrUnauthenticated, h.Typ)
	case h.Crit != nil:
		return Claims{}, fmt.Errorf("%w: critical header extensions not supported", ErrUnauthenticated)
	case len(sig) != 64:
		return Claims{}, fmt.Errorf("%w: malformed signature", ErrUnauthenticated)
	}
	key, err := v.key(ctx, h.Kid)
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
	var c tokenClaims
	if err := strictJSON(payload, &c); err != nil {
		return Claims{}, fmt.Errorf("%w: malformed claims", ErrUnauthenticated)
	}
	now := v.now()
	leeway := orDefault(v.Leeway, 30*time.Second)
	exp, okExp := unix(c.Exp)
	nbf, okNbf := unix(c.Nbf)
	iat, okIat := unix(c.Iat)
	switch {
	case c.Iss == nil || *c.Iss != v.Issuer:
		return Claims{}, fmt.Errorf("%w: wrong issuer", ErrUnauthenticated)
	case !hasAudience(c.Aud, Audience):
		return Claims{}, fmt.Errorf("%w: wrong audience", ErrUnauthenticated)
	case !okExp:
		return Claims{}, fmt.Errorf("%w: no expiry", ErrUnauthenticated)
	case !now.Before(exp.Add(leeway)):
		return Claims{}, fmt.Errorf("%w: expired", ErrUnauthenticated)
	case c.Nbf != nil && (!okNbf || nbf.After(now.Add(leeway))):
		return Claims{}, fmt.Errorf("%w: not valid yet", ErrUnauthenticated)
	case c.Iat != nil && (!okIat || iat.After(now.Add(leeway))):
		return Claims{}, fmt.Errorf("%w: issued in the future", ErrUnauthenticated)
	case c.Sub == nil || !uuidPattern.MatchString(*c.Sub):
		return Claims{}, fmt.Errorf("%w: no user subject", ErrUnauthenticated)
	case c.SessionID == nil || !uuidPattern.MatchString(*c.SessionID):
		return Claims{}, fmt.Errorf("%w: no session", ErrUnauthenticated)
	case c.Role == nil || *c.Role != Audience:
		return Claims{}, fmt.Errorf("%w: not a signed-in user's token", ErrUnauthenticated)
	case c.IsAnonymous != nil && *c.IsAnonymous:
		return Claims{}, fmt.Errorf("%w: anonymous session", ErrUnauthenticated)
	}
	return Claims{Subject: *c.Sub, SessionID: *c.SessionID, ExpiresAt: exp}, nil
}

// strictJSON decodes exactly one JSON object with numbers kept exact.
// Unknown fields are allowed (providers add claims); duplicates are not
// rejected by encoding/json, so the last value wins as in every major JWT
// library.
func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

func unix(n *json.Number) (time.Time, bool) {
	if n == nil {
		return time.Time{}, false
	}
	f, err := n.Float64()
	if err != nil || f < 0 || f > 1e11 {
		return time.Time{}, false
	}
	sec := int64(f)
	return time.Unix(sec, int64((f-float64(sec))*1e9)), true
}

func hasAudience(raw json.RawMessage, want string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == want
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == want {
				return true
			}
		}
	}
	return false
}

// key returns the public key for kid, refreshing the key set when the kid is
// unknown or the set is older than MaxKeyAge (rate-limited).
func (v *Verifier) key(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	k, known := v.keys[kid]
	stale := now.Sub(v.fetchedAt) > orDefault(v.MaxKeyAge, 10*time.Minute)
	if known && !stale {
		return k, nil
	}
	if v.lastAttempt.IsZero() || now.Sub(v.lastAttempt) >= orDefault(v.RefreshInterval, 30*time.Second) {
		v.lastAttempt = now
		keys, err := v.fetch(ctx)
		switch {
		case err == nil:
			v.keys, v.fetchedAt = keys, now
			k, known = keys[kid]
		case v.keys == nil:
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		// A failed refresh keeps the cached set: a provider outage must not
		// reject tokens signed with a key that was current.
	}
	switch {
	case v.keys == nil: // never fetched successfully; retried after the interval
		return nil, fmt.Errorf("%w: signing keys not available yet", ErrUnavailable)
	case !known:
		return nil, fmt.Errorf("%w: unknown signing key", ErrUnauthenticated)
	}
	return k, nil
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
		client = &http.Client{Timeout: 5 * time.Second}
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
		x, errX := base64.RawURLEncoding.DecodeString(k.X)
		y, errY := base64.RawURLEncoding.DecodeString(k.Y)
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
