package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionCheckAsksTheProvider(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"current session":     {http.StatusOK, `{"id":"` + testSubject + `"}`, nil},
		"another user":        {http.StatusOK, `{"id":"22222222-2222-4222-8222-222222222222"}`, ErrSessionEnded},
		"unreadable user":     {http.StatusOK, `not json`, ErrSessionEnded},
		"signed out":          {http.StatusForbidden, `{"code":403,"error_code":"session_not_found"}`, ErrSessionEnded},
		"token rejected":      {http.StatusUnauthorized, `{}`, ErrSessionEnded},
		"provider error":      {http.StatusInternalServerError, `{}`, ErrUnavailable},
		"provider overloaded": {http.StatusTooManyRequests, `{}`, ErrUnavailable},
	} {
		var gotKey, gotAuth, gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKey, gotAuth, gotPath = r.Header.Get("apikey"), r.Header.Get("Authorization"), r.URL.Path
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		s := &SessionChecker{Issuer: srv.URL + "/auth/v1", PublishableKey: "sb_publishable_test", HTTP: srv.Client()}
		err := s.Check(context.Background(), "the.access.token", testSubject)
		srv.Close()
		if (c.want == nil && err != nil) || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
		if gotPath != "/auth/v1/user" || gotKey != "sb_publishable_test" || gotAuth != "Bearer the.access.token" {
			t.Errorf("%s: request %s apikey=%q auth=%q", name, gotPath, gotKey, gotAuth)
		}
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // unreachable
	s := &SessionChecker{Issuer: srv.URL + "/auth/v1", PublishableKey: "k", HTTP: srv.Client()}
	if err := s.Check(context.Background(), "t", testSubject); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unreachable provider: %v, want ErrUnavailable (fail closed)", err)
	}
}

func TestActorCapabilities(t *testing.T) {
	a := Actor{Capabilities: []string{Fulfillment}}
	if !a.Staff() || !a.Has(Fulfillment) || a.Has(Chef) {
		t.Fatalf("fulfillment actor: staff=%v fulfillment=%v chef=%v", a.Staff(), a.Has(Fulfillment), a.Has(Chef))
	}
	if (Actor{}).Staff() {
		t.Fatal("an actor without capabilities is staff")
	}
}
