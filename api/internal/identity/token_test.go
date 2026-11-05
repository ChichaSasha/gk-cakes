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

// clock is a settable time source, safe for the background refresh.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// keyServer serves a JWKS of the given keys and counts fetches.
type keyServer struct {
	mu      sync.Mutex
	keys    map[string]*ecdsa.PrivateKey
	extra   []map[string]string // raw entries added to the set
	status  int
	body    []byte        // when set, served as is
	delay   time.Duration // before answering
	fetches int
}

func (s *keyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.fetches++
	delay := s.delay
	s.mu.Unlock()
	time.Sleep(delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
		http.NotFound(w, r)
		return
	}
	if s.status != 0 {
		w.WriteHeader(s.status)
	}
	if s.body != nil {
		_, _ = w.Write(s.body)
		return
	}
	if s.status != 0 {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": s.set()})
}

func (s *keyServer) set() []any {
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
	return set
}

func (s *keyServer) edit(f func(s *keyServer)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

func (s *keyServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetches
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
	clock  *clock
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{k1: newKey(t), clock: &clock{t: time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)}}
	f.keys = &keyServer{keys: map[string]*ecdsa.PrivateKey{"k1": f.k1}}
	f.srv = httptest.NewServer(f.keys)
	t.Cleanup(f.srv.Close)
	f.issuer = f.srv.URL + "/auth/v1"
	f.v = &Verifier{Issuer: f.issuer, HTTP: f.srv.Client(), Now: f.clock.Now}
	t.Cleanup(f.idle)
	return f
}

// idle waits for a background refresh to end.
func (f *fixture) idle() {
	f.v.mu.Lock()
	ch := f.v.inflight
	f.v.mu.Unlock()
	if ch != nil {
		<-ch
	}
}

func (f *fixture) claims() map[string]any {
	now := f.clock.Now()
	return map[string]any{
		"iss": f.issuer, "aud": "authenticated", "sub": testSubject, "session_id": testSession,
		"role": "authenticated", "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "is_anonymous": false,
		"email": "customer@example.com", "app_metadata": map[string]any{"provider": "email"},
	}
}

func b64json(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

// signRaw signs the given encoded header and payload segments with key.
func signRaw(t *testing.T, key *ecdsa.PrivateKey, header, payload string) string {
	t.Helper()
	input := header + "." + payload
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

// sign builds an ES256 token over header and claims with key.
func sign(t *testing.T, key *ecdsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	return signRaw(t, key, b64json(header), b64json(claims))
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

// rawClaims signs a hand-written claims JSON (for encodings json.Marshal
// cannot produce, such as duplicate names).
func (f *fixture) rawClaims(t *testing.T, claims string) string {
	t.Helper()
	return signRaw(t, f.k1, b64json(map[string]any{"alg": "ES256", "kid": "k1"}),
		base64.RawURLEncoding.EncodeToString([]byte(claims)))
}

func TestVerifyAcceptsAProviderToken(t *testing.T) {
	f := newFixture(t)
	c, err := f.v.Verify(context.Background(), f.token(t, nil))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.Subject != testSubject || c.SessionID != testSession || !c.ExpiresAt.Equal(f.clock.Now().Add(time.Hour)) {
		t.Fatalf("claims = %+v", c)
	}
	arr := f.token(t, func(_, c map[string]any) { c["aud"] = []string{"other", "authenticated"} })
	if _, err := f.v.Verify(context.Background(), arr); err != nil {
		t.Fatalf("audience list containing authenticated: %v", err)
	}
	now := f.clock.Now()
	skew := f.token(t, func(_, c map[string]any) {
		c["exp"] = now.Add(-10 * time.Second).Unix()
		c["nbf"] = now.Add(10 * time.Second).Unix()
	})
	if _, err := f.v.Verify(context.Background(), skew); err != nil {
		t.Fatalf("within the clock-skew leeway: %v", err)
	}
	if _, err := f.v.Verify(context.Background(), f.token(t, func(_, c map[string]any) { delete(c, "is_anonymous") })); err != nil {
		t.Fatalf("no is_anonymous claim: %v", err)
	}
}

func TestVerifyRejectsWrongTokens(t *testing.T) {
	f := newFixture(t)
	other := newKey(t)
	now := f.clock.Now()
	exp := now.Add(time.Hour).Unix()
	valid := func(extra string) string {
		return `{"iss":"` + f.issuer + `","aud":"authenticated","sub":"` + testSubject + `","session_id":"` + testSession +
			`","role":"authenticated","exp":` + jsonNum(exp) + extra + `}`
	}
	cases := map[string]string{
		"wrong issuer":       f.token(t, func(_, c map[string]any) { c["iss"] = "https://evil.example/auth/v1" }),
		"no issuer":          f.token(t, func(_, c map[string]any) { delete(c, "iss") }),
		"wrong audience":     f.token(t, func(_, c map[string]any) { c["aud"] = "anon" }),
		"audience list":      f.token(t, func(_, c map[string]any) { c["aud"] = []string{"anon"} }),
		"no audience":        f.token(t, func(_, c map[string]any) { delete(c, "aud") }),
		"expired":            f.token(t, func(_, c map[string]any) { c["exp"] = now.Add(-31 * time.Second).Unix() }),
		"no expiry":          f.token(t, func(_, c map[string]any) { delete(c, "exp") }),
		"quoted expiry":      f.token(t, func(_, c map[string]any) { c["exp"] = jsonNum(exp) }),
		"absurd expiry":      f.token(t, func(_, c map[string]any) { c["exp"] = 2e11 }), // year 8300
		"not yet valid":      f.token(t, func(_, c map[string]any) { c["nbf"] = now.Add(time.Minute).Unix() }),
		"quoted not-before":  f.token(t, func(_, c map[string]any) { c["nbf"] = "0" }),
		"issued in future":   f.token(t, func(_, c map[string]any) { c["iat"] = now.Add(time.Minute).Unix() }),
		"no subject":         f.token(t, func(_, c map[string]any) { delete(c, "sub") }),
		"subject not uuid":   f.token(t, func(_, c map[string]any) { c["sub"] = "admin" }),
		"no session":         f.token(t, func(_, c map[string]any) { delete(c, "session_id") }),
		"session not uuid":   f.token(t, func(_, c map[string]any) { c["session_id"] = "x" }),
		"anon role":          f.token(t, func(_, c map[string]any) { c["role"] = "anon" }),
		"service role":       f.token(t, func(_, c map[string]any) { c["role"] = "service_role" }),
		"no role":            f.token(t, func(_, c map[string]any) { delete(c, "role") }),
		"anonymous user":     f.token(t, func(_, c map[string]any) { c["is_anonymous"] = true }),
		"anonymous as text":  f.token(t, func(_, c map[string]any) { c["is_anonymous"] = "false" }),
		"HS256":              f.token(t, func(h, _ map[string]any) { h["alg"] = "HS256" }),
		"none":               f.token(t, func(h, _ map[string]any) { h["alg"] = "none" }),
		"RS256":              f.token(t, func(h, _ map[string]any) { h["alg"] = "RS256" }),
		"no algorithm":       f.token(t, func(h, _ map[string]any) { delete(h, "alg") }),
		"no key ID":          f.token(t, func(h, _ map[string]any) { delete(h, "kid") }),
		"numeric key ID":     f.token(t, func(h, _ map[string]any) { h["kid"] = 1 }),
		"unknown key ID":     f.token(t, func(h, _ map[string]any) { h["kid"] = "k9" }),
		"other type":         f.token(t, func(h, _ map[string]any) { h["typ"] = "at+jwt" }),
		"numeric type":       f.token(t, func(h, _ map[string]any) { h["typ"] = 1 }),
		"critical header":    f.token(t, func(h, _ map[string]any) { h["crit"] = []string{"exp"} }),
		"other key":          sign(t, other, map[string]any{"alg": "ES256", "kid": "k1"}, f.claims()),
		"upper-case names":   f.rawClaims(t, strings.Replace(strings.Replace(valid(""), `"sub"`, `"SUB"`, 1), `"role"`, `"Role"`, 1)),
		"duplicate subject":  f.rawClaims(t, valid(`,"sub":"22222222-2222-4222-8222-222222222222"`)),
		"trailing data":      f.rawClaims(t, valid("")+` {}`),
		"claims not object":  f.rawClaims(t, `["sub"]`),
		"two parts":          "a.b",
		"bad encoding":       "@@@.@@@.@@@",
		"padded encoding":    f.token(t, nil) + "==",
		"oversized":          strings.Repeat("a", MaxTokenBytes+1),
		"empty":              "",
		"duplicate kid head": signRaw(t, f.k1, base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","kid":"k1","kid":"k1"}`)), b64json(f.claims())),
	}
	good := f.token(t, nil)
	parts := strings.Split(good, ".")
	cases["tampered claims"] = parts[0] + "." + b64json(map[string]any{"sub": "22222222-2222-4222-8222-222222222222"}) + "." + parts[2]
	der := base64.RawURLEncoding.EncodeToString(append([]byte{0x30}, make([]byte, 70)...))
	cases["DER signature"] = parts[0] + "." + parts[1] + "." + der
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	// A zero byte between r and s keeps both values: only the exact length
	// refuses this non-canonical encoding.
	padded := append(append(append([]byte{}, sig[:32]...), 0), sig[32:]...)
	cases["padded signature"] = parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(padded)
	// The unused low bits of the last base64 character are not canonical.
	last := parts[2][len(parts[2])-1]
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	flipped := alphabet[strings.IndexByte(alphabet, last)^1]
	cases["non-canonical signature encoding"] = parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-1] + string(flipped)
	for name, tok := range cases {
		if _, err := f.v.Verify(context.Background(), tok); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s: %v, want ErrUnauthenticated", name, err)
		} else if tok != "" && strings.Contains(err.Error(), tok) {
			t.Errorf("%s: error contains the token", name)
		}
	}
}

func jsonNum(n int64) string { b, _ := json.Marshal(n); return string(b) }

// A new key ID (rotation) waits for a refresh; repeated unknown IDs do not
// hammer the provider within the refresh interval.
func TestVerifyFollowsKeyRotation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatal(err)
	}
	k2 := newKey(t)
	rotated := sign(t, k2, map[string]any{"alg": "ES256", "kid": "k2"}, f.claims())
	f.keys.edit(func(s *keyServer) { s.keys["k2"] = k2 })
	if _, err := f.v.Verify(context.Background(), rotated); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("within the refresh interval: %v, want still unknown", err)
	}
	if n := f.keys.count(); n != 1 {
		t.Fatalf("fetches = %d, want 1: unknown key IDs within the refresh interval do not refetch", n)
	}
	f.clock.Add(31 * time.Second)
	if _, err := f.v.Verify(context.Background(), rotated); err != nil {
		t.Fatalf("after the refresh interval, the published new key: %v", err)
	}
	if n := f.keys.count(); n != 2 {
		t.Fatalf("fetches = %d, want 2", n)
	}
	// A withdrawn key stops being accepted once the cached set is too old.
	f.keys.edit(func(s *keyServer) { delete(s.keys, "k1") })
	f.clock.Add(11 * time.Minute)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("withdrawn key after the max key age: %v", err)
	}
}

// A request that triggers a refresh and is then cancelled cannot abort it,
// so a client disconnect never blocks a rotation or keeps a withdrawn key.
func TestCancelledRequestCannotStopARefresh(t *testing.T) {
	f := newFixture(t)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatal(err)
	}
	k2 := newKey(t)
	f.keys.edit(func(s *keyServer) { s.keys["k2"] = k2; s.delay = 100 * time.Millisecond })
	f.clock.Add(31 * time.Second)
	rotated := sign(t, k2, map[string]any{"alg": "ES256", "kid": "k2"}, f.claims())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.v.Verify(ctx, rotated); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cancelled request: %v, want ErrUnavailable", err)
	}
	f.idle()
	if _, err := f.v.Verify(context.Background(), rotated); err != nil {
		t.Fatalf("after the background refresh: %v, want the rotated key accepted", err)
	}
	if n := f.keys.count(); n != 2 {
		t.Fatalf("fetches = %d, want 2", n)
	}
}

// A slow provider never delays a token whose key is cached and fresh.
func TestSlowRefreshDoesNotBlockCachedKeys(t *testing.T) {
	f := newFixture(t)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatal(err)
	}
	f.keys.edit(func(s *keyServer) { s.delay = 2 * time.Second })
	f.clock.Add(31 * time.Second)
	unknown := sign(t, newKey(t), map[string]any{"alg": "ES256", "kid": "k9"}, f.claims())
	go func() { _, _ = f.v.Verify(context.Background(), unknown) }() // waits on the slow refresh
	for f.keys.count() < 2 {
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("a cached-key token waited %v behind a refresh", d)
	}
}

func TestVerifyFailsClosedWithoutUsableKeys(t *testing.T) {
	f := newFixture(t)
	f.keys.edit(func(s *keyServer) { s.status = http.StatusBadGateway })
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no key set ever fetched: %v, want ErrUnavailable", err)
	}
	f.keys.edit(func(s *keyServer) { s.status = 0 })
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); !errors.Is(err, ErrUnavailable) || f.keys.count() != 1 {
		t.Fatalf("retry within the retry interval: %v after %d fetches, want ErrUnavailable without a new fetch", err, f.keys.count())
	}
	f.clock.Add(6 * time.Second) // the short retry while no set was ever fetched
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("provider back after the retry interval: %v", err)
	}
	// Later outage: the stale cached set keeps working, up to the hard age.
	f.keys.edit(func(s *keyServer) { s.status = http.StatusBadGateway })
	f.clock.Add(11 * time.Minute)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("outage with a stale cached key: %v, want the cached key used", err)
	}
	f.clock.Add(50 * time.Minute)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("outage beyond the hard key age: %v, want ErrUnavailable", err)
	}
}

func TestFetchKeepsOnlyES256SigningKeys(t *testing.T) {
	f := newFixture(t)
	// Valid P-256 coordinates under labels that must be refused, so each
	// filter is what rejects them.
	pub, _ := newKey(t).PublicKey.Bytes()
	x, y := base64.RawURLEncoding.EncodeToString(pub[1:33]), base64.RawURLEncoding.EncodeToString(pub[33:])
	f.keys.edit(func(s *keyServer) {
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
	good, _ := json.Marshal(map[string]any{"keys": []map[string]string{{"kty": "EC", "crv": "P-256", "kid": "k1", "x": x, "y": y}}})
	f.keys.edit(func(s *keyServer) { s.status, s.body = http.StatusNotFound, good })
	if _, err := f.v.fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("valid body with status 404: %v, want refused", err)
	}
	huge := append(append([]byte(`{"keys":[],"pad":"`), []byte(strings.Repeat("x", maxJWKSBytes))...), []byte(`"}`)...)
	f.keys.edit(func(s *keyServer) { s.status, s.body = 0, huge })
	if _, err := f.v.fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "oversized") {
		t.Fatalf("oversized key set: %v, want refused", err)
	}
	f.keys.edit(func(s *keyServer) { s.status, s.body = 0, good })
	if keys, err := f.v.fetch(context.Background()); err != nil || len(keys) != 1 {
		t.Fatalf("the same key with status 200: %v, %v", keys, err)
	}
}

func TestProviderRedirectsAreNotFollowed(t *testing.T) {
	f := newFixture(t)
	redirect := httptest.NewServer(http.RedirectHandler(f.srv.URL+"/auth/v1/.well-known/jwks.json", http.StatusFound))
	t.Cleanup(redirect.Close)
	v := &Verifier{Issuer: redirect.URL + "/auth/v1", HTTP: &http.Client{CheckRedirect: NoRedirects}}
	if _, err := v.fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "status 302") {
		t.Fatalf("redirected key set: %v, want refused", err)
	}
}

// Once a refresh has failed, a stale cached key is served without waiting
// for each retry, which runs in the background.
func TestStaleKeyIsServedWithoutWaitingAfterAFailedRefresh(t *testing.T) {
	f := newFixture(t)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatal(err)
	}
	f.keys.edit(func(s *keyServer) { s.status, s.delay = http.StatusBadGateway, 300*time.Millisecond })
	f.clock.Add(11 * time.Minute) // stale: the first request waits for the (failing) refresh
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("stale key after a failed refresh: %v", err)
	}
	f.clock.Add(31 * time.Second) // the next retry is due
	start := time.Now()
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("stale key while retrying: %v", err)
	}
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Fatalf("waited %v for a retry after a failed refresh; want the stale key at once", d)
	}
	f.idle() // the retry still runs, in the background
	if n := f.keys.count(); n != 3 {
		t.Fatalf("fetches = %d, want 3 (the retry still runs)", n)
	}
}

// Every fetch has its own timeout, so a hanging provider cannot keep a
// refresh running forever and block all later ones.
func TestHangingProviderTimesOut(t *testing.T) {
	f := newFixture(t)
	f.keys.edit(func(s *keyServer) { s.delay = 2 * time.Second })
	f.v.FetchTimeout = 50 * time.Millisecond
	start := time.Now()
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("hanging provider: %v, want ErrUnavailable", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("waited %v; want the fetch timeout", d)
	}
	f.idle()
	f.keys.edit(func(s *keyServer) { s.delay = 0 })
	f.clock.Add(6 * time.Second)
	if _, err := f.v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("provider back: %v, want a new refresh to start", err)
	}
}

// The sign-in instant is the latest email-link/OTP or Google amr timestamp
// (Supabase keeps it across refreshes), never a password one and never iat; a missing or unusable amr leaves it zero without
// rejecting the token; a future timestamp beyond the leeway is ignored.
func TestAuthenticatedAtComesFromAmr(t *testing.T) {
	f := newFixture(t)
	now := f.clock.Now()
	at := func(edit func(c map[string]any)) time.Time {
		t.Helper()
		c, err := f.v.Verify(context.Background(), f.token(t, func(_, c map[string]any) { edit(c) }))
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		return c.AuthenticatedAt
	}
	signedIn := now.Add(-3 * time.Hour)
	if got := at(func(c map[string]any) {
		c["amr"] = []map[string]any{{"method": "otp", "timestamp": now.Add(-30 * 24 * time.Hour).Unix()},
			{"method": "oauth", "timestamp": signedIn.Unix()}}
	}); !got.Equal(signedIn) {
		t.Fatalf("latest amr: %s, want %s", got, signedIn)
	}
	// A later password sign-in (staff only) or refresh does not count.
	if got := at(func(c map[string]any) {
		c["amr"] = []map[string]any{{"method": "magiclink", "timestamp": signedIn.Unix()},
			{"method": "password", "timestamp": now.Add(-time.Minute).Unix()},
			{"method": "token_refresh", "timestamp": now.Unix()}}
	}); !got.Equal(signedIn) {
		t.Fatalf("password after the email link: %s, want %s", got, signedIn)
	}
	for name, edit := range map[string]func(c map[string]any){
		"no amr":         func(map[string]any) {},
		"password only":  func(c map[string]any) { c["amr"] = []map[string]any{{"method": "password", "timestamp": now.Unix()}} },
		"recovery only":  func(c map[string]any) { c["amr"] = []map[string]any{{"method": "recovery", "timestamp": now.Unix()}} },
		"amr not a list": func(c map[string]any) { c["amr"] = "otp" },
		"no method":      func(c map[string]any) { c["amr"] = []map[string]any{{"timestamp": now.Unix()}} },
		"string time":    func(c map[string]any) { c["amr"] = []map[string]any{{"method": "otp", "timestamp": "now"}} },
		"future": func(c map[string]any) {
			c["amr"] = []map[string]any{{"method": "otp", "timestamp": now.Add(time.Hour).Unix()}}
		},
	} {
		if got := at(edit); !got.IsZero() {
			t.Errorf("%s: %s, want zero (iat is never used)", name, got)
		}
	}
}
