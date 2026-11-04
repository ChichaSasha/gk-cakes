package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testSubject = "11111111-2222-4333-8444-555555555555"
	testSession = "66666666-7777-4888-9999-aaaaaaaaaaaa"
)

var testNow = time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)

// keyServer serves a JWKS of the given keys and counts fetches.
type keyServer struct {
	mu      sync.Mutex
	keys    map[string]*ecdsa.PrivateKey
	extra   []map[string]string // raw entries added to the set
	status  int
	fetches int
}

func (s *keyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetches++
	if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
		http.NotFound(w, r)
		return
	}
	if s.status != 0 {
		w.WriteHeader(s.status)
		return
	}
	var set []any
	for kid, k := range s.keys {
		pub, _ := k.PublicKey.Bytes() // 0x04 || X || Y
		set = append(set, map[string]string{
			"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": kid,
			"x": base64.RawURLEncoding.EncodeToString(pub[1:33]),
			"y": base64.RawURLEncoding.EncodeToString(pub[33:]),
		})
	}
	for _, e := range s.extra {
		set = append(set, e)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": set})
}

func (s *keyServer) set(f func(s *keyServer)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// fixture is a verifier against a key server holding key "k1".
type fixture struct {
	srv    *httptest.Server
	keys   *keyServer
	k1     *ecdsa.PrivateKey
	v      *Verifier
	issuer string
	now    time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{k1: newKey(t), now: testNow}
	f.keys = &keyServer{keys: map[string]*ecdsa.PrivateKey{"k1": f.k1}}
	f.srv = httptest.NewServer(f.keys)
	t.Cleanup(f.srv.Close)
	f.issuer = f.srv.URL + "/auth/v1"
	f.v = &Verifier{Issuer: f.issuer, HTTP: f.srv.Client(), Now: func() time.Time { return f.now }}
	return f
}

func (f *fixture) claims() map[string]any {
	return map[string]any{
		"iss": f.issuer, "aud": "authenticated", "sub": testSubject, "session_id": testSession,
		"role": "authenticated", "exp": f.now.Add(time.Hour).Unix(), "iat": f.now.Unix(), "is_anonymous": false,
		"email": "customer@example.com", "app_metadata": map[string]any{"provider": "email"},
	}
}

func b64(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

// sign builds an ES256 token over header and claims with key.
func sign(t *testing.T, key *ecdsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	input := b64(header) + "." + b64(claims)
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fixture) token(t *testing.T, edit func(h, c map[string]any)) string {
	t.Helper()
	h := map[string]any{"alg": "ES256", "kid": "k1", "typ": "JWT"}
	c := f.claims()
	if edit != nil {
		edit(h, c)
	}
	return sign(t, f.k1, h, c)
}

func TestVerifyAcceptsAProviderToken(t *testing.T) {
	f := newFixture(t)
	c, err := f.v.Verify(context.Background(), f.token(t, nil))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.Subject != testSubject || c.SessionID != testSession || !c.ExpiresAt.Equal(f.now.Add(time.Hour)) {
		t.Fatalf("claims = %+v", c)
	}
	arr := f.token(t, func(_, c map[string]any) { c["aud"] = []string{"other", "authenticated"} })
	if _, err := f.v.Verify(context.Background(), arr); err != nil {
		t.Fatalf("audience list containing authenticated: %v", err)
	}
	skew := f.token(t, func(_, c map[string]any) {
		c["exp"] = f.now.Add(-10 * time.Second).Unix()
		c["nbf"] = f.now.Add(10 * time.Second).Unix()
	})
	if _, err := f.v.Verify(context.Background(), skew); err != nil {
		t.Fatalf("within the clock-skew leeway: %v", err)
	}
}

func TestVerifyRejectsWrongTokens(t *testing.T) {
	f := newFixture(t)
	other := newKey(t)
	cases := map[string]string{
		"wrong issuer":     f.token(t, func(_, c map[string]any) { c["iss"] = "https://evil.example/auth/v1" }),
		"no issuer":        f.token(t, func(_, c map[string]any) { delete(c, "iss") }),
		"wrong audience":   f.token(t, func(_, c map[string]any) { c["aud"] = "anon" }),
		"audience list":    f.token(t, func(_, c map[string]any) { c["aud"] = []string{"anon"} }),
		"no audience":      f.token(t, func(_, c map[string]any) { delete(c, "aud") }),
		"expired":          f.token(t, func(_, c map[string]any) { c["exp"] = f.now.Add(-31 * time.Second).Unix() }),
		"no expiry":        f.token(t, func(_, c map[string]any) { delete(c, "exp") }),
		"string expiry":    f.token(t, func(_, c map[string]any) { c["exp"] = "never" }),
		"not yet valid":    f.token(t, func(_, c map[string]any) { c["nbf"] = f.now.Add(time.Minute).Unix() }),
		"issued in future": f.token(t, func(_, c map[string]any) { c["iat"] = f.now.Add(time.Minute).Unix() }),
		"no subject":       f.token(t, func(_, c map[string]any) { delete(c, "sub") }),
		"subject not uuid": f.token(t, func(_, c map[string]any) { c["sub"] = "admin" }),
		"no session":       f.token(t, func(_, c map[string]any) { delete(c, "session_id") }),
		"session not uuid": f.token(t, func(_, c map[string]any) { c["session_id"] = "x" }),
		"anon role":        f.token(t, func(_, c map[string]any) { c["role"] = "anon" }),
		"service role":     f.token(t, func(_, c map[string]any) { c["role"] = "service_role" }),
		"no role":          f.token(t, func(_, c map[string]any) { delete(c, "role") }),
		"anonymous user":   f.token(t, func(_, c map[string]any) { c["is_anonymous"] = true }),
		"HS256":            f.token(t, func(h, _ map[string]any) { h["alg"] = "HS256" }),
		"none":             f.token(t, func(h, _ map[string]any) { h["alg"] = "none" }),
		"RS256":            f.token(t, func(h, _ map[string]any) { h["alg"] = "RS256" }),
		"no key ID":        f.token(t, func(h, _ map[string]any) { delete(h, "kid") }),
		"unknown key ID":   f.token(t, func(h, _ map[string]any) { h["kid"] = "k9" }),
		"other type":       f.token(t, func(h, _ map[string]any) { h["typ"] = "at+jwt" }),
		"critical header":  f.token(t, func(h, _ map[string]any) { h["crit"] = []string{"exp"} }),
		"other key":        sign(t, other, map[string]any{"alg": "ES256", "kid": "k1"}, f.claims()),
		"two parts":        "a.b",
		"bad encoding":     "@@@.@@@.@@@",
		"oversized":        strings.Repeat("a", MaxTokenBytes+1),
		"empty":            "",
	}
	good := f.token(t, nil)
	parts := strings.Split(good, ".")
	cases["tampered claims"] = parts[0] + "." + b64(map[string]any{"sub": "22222222-2222-4222-8222-222222222222"}) + "." + parts[2]
	cases["trailing data"] = parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x"} {}`)) + "." + parts[2]
	der := base64.RawURLEncoding.EncodeToString(append([]byte{0x30}, make([]byte, 70)...))
	cases["DER signature"] = parts[0] + "." + parts[1] + "." + der
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	// A zero byte between r and s keeps both values: only the exact length
	// refuses this non-canonical encoding.
	padded := append(append(append([]byte{}, sig[:32]...), 0), sig[32:]...)
	cases["padded signature"] = parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(padded)
	for name, tok := range cases {
		if _, err := f.v.Verify(context.Background(), tok); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s: %v, want ErrUnauthenticated", name, err)
		} else if strings.Contains(err.Error(), tok) && tok != "" {
			t.Errorf("%s: error contains the token", name)
		}
	}
}

// A new key ID (rotation) refetches the set once; repeated unknown IDs do
// not hammer the provider within the refresh interval.
func TestVerifyFollowsKeyRotation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatal(err)
	}
	k2 := newKey(t)
	rotated := sign(t, k2, map[string]any{"alg": "ES256", "kid": "k2"}, f.claims())
	if _, err := f.v.Verify(context.Background(), rotated); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("unknown key before the provider publishes it: %v", err)
	}
	f.keys.set(func(s *keyServer) { s.keys["k2"] = k2 })
	if _, err := f.v.Verify(context.Background(), rotated); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("within the refresh interval: %v, want still unknown", err)
	}
	if f.keys.fetches != 1 {
		t.Fatalf("fetches = %d, want 1: unknown key IDs within the refresh interval do not refetch", f.keys.fetches)
	}
	f.now = f.now.Add(31 * time.Second)
	if _, err := f.v.Verify(context.Background(), rotated); err != nil {
		t.Fatalf("after the refresh interval, the published new key: %v", err)
	}
	if f.keys.fetches != 2 {
		t.Fatalf("fetches = %d, want 2", f.keys.fetches)
	}
	// A withdrawn key stops being accepted once the cached set is too old.
	f.keys.set(func(s *keyServer) { delete(s.keys, "k1") })
	f.now = f.now.Add(11 * time.Minute)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("withdrawn key after the max key age: %v", err)
	}
}

func TestVerifyFailsClosedWithoutKeys(t *testing.T) {
	f := newFixture(t)
	f.keys.set(func(s *keyServer) { s.status = http.StatusBadGateway })
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no key set ever fetched: %v, want ErrUnavailable", err)
	}
	f.keys.set(func(s *keyServer) { s.status = 0 })
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("retry within the interval: %v, want ErrUnavailable without a new fetch", err)
	}
	f.now = f.now.Add(31 * time.Second)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("provider back: %v", err)
	}
	// Later outage: the cached set keeps working.
	f.keys.set(func(s *keyServer) { s.status = http.StatusBadGateway })
	f.now = f.now.Add(11 * time.Minute)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("outage with a cached key: %v, want the cached key used", err)
	}
}

func TestFetchKeepsOnlyES256SigningKeys(t *testing.T) {
	f := newFixture(t)
	// Valid P-256 coordinates under labels that must be refused, so each
	// filter is what rejects them.
	pub, _ := newKey(t).PublicKey.Bytes()
	x, y := base64.RawURLEncoding.EncodeToString(pub[1:33]), base64.RawURLEncoding.EncodeToString(pub[33:])
	f.keys.set(func(s *keyServer) {
		s.keys = map[string]*ecdsa.PrivateKey{}
		s.extra = []map[string]string{
			{"kty": "RSA", "kid": "rsa", "x": x, "y": y},
			{"kty": "EC", "crv": "P-384", "kid": "p384", "x": x, "y": y},
			{"kty": "EC", "crv": "P-256", "kid": "es384", "alg": "ES384", "x": x, "y": y},
			{"kty": "EC", "crv": "P-256", "kid": "enc", "use": "enc", "x": x, "y": y},
			{"kty": "EC", "crv": "P-256", "kid": "bad kid!", "x": x, "y": y},
			{"kty": "EC", "crv": "P-256", "kid": "short", "x": x[:20], "y": y},
			{"kty": "EC", "crv": "P-256", "kid": "offcurve", "x": base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
				"y": base64.RawURLEncoding.EncodeToString(make([]byte, 32))},
		}
	})
	if _, err := f.v.fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "no ES256 signing key") {
		t.Fatalf("fetch: %v, want no usable key", err)
	}
}
