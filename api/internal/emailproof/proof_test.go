package emailproof

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbesNeedRefusals(t *testing.T) {
	var got []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		got = append(got, r.Header.Clone())
		w.WriteHeader(http.StatusOK) // an endpoint that accepts anything
	}))
	defer srv.Close()
	cfg := Config{APIURL: srv.URL, HTTP: srv.Client(), Label: "t1", Environment: "test", Now: time.Now}
	probes, err := probeEndpoint(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if problems := probeProblems(probes); len(problems) != 4 {
		t.Fatalf("problems %v, want all four probes reported", problems)
	}
	if got[0].Get("svix-signature") != "" || got[1].Get("svix-signature") == "" || got[2].Get("svix-timestamp") >= got[1].Get("svix-timestamp") {
		t.Fatalf("probe headers %v: want unsigned, then forged, then stale", got)
	}
	ok := []Probe{{Status: 401, Want: 401}, {Status: 413, Want: 413}}
	if problems := probeProblems(ok); len(problems) != 0 {
		t.Fatalf("refused probes reported %v", problems)
	}
}

func TestProbeReportsAnUnservedWebhook(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := probeEndpoint(context.Background(), Config{APIURL: srv.URL, HTTP: srv.Client(), Label: "t1", Environment: "test", Now: time.Now})
	if err == nil || !strings.Contains(err.Error(), "EMAIL_WEBHOOK_SECRET is not set") {
		t.Fatalf("err = %v", err)
	}
}

type lines []string

func (l lines) Since(context.Context, time.Time) ([]string, error) { return l, nil }

type failing struct{}

func (failing) Since(context.Context, time.Time) ([]string, error) {
	return nil, errors.New("Not Authorized")
}

func TestLogScanNeedsTheRunAndNoPersonalData(t *testing.T) {
	cases := []*Case{{To: "delivered+t1@resend.dev"}, {To: "owner@example.org"}}
	subject := "GK Cakes sandbox proof t1"
	clean := lines{"email provider call result=accepted", "email webhook event result=recorded", "request path=/readyz"}
	if r, problems := checkLogs(context.Background(), Config{Label: "t1", Logs: clean}, time.Now(), cases, subject); len(problems) != 0 || !r.verified {
		t.Fatalf("clean logs: %+v, %v", r, problems)
	}
	for name, l := range map[string]lines{
		"recipient":         append(lines{"debug to=Delivered+T1@resend.dev"}, clean...),
		"local part":        append(lines{"debug owner"}, clean...),
		"subject":           append(lines{"subject=GK Cakes sandbox proof t1"}, clean...),
		"no dispatch lines": {"email webhook event result=recorded"},
		"no webhook lines":  {"email provider call result=accepted"},
		"empty":             {},
	} {
		if _, problems := checkLogs(context.Background(), Config{Label: "t1", Logs: l}, time.Now(), cases, subject); len(problems) == 0 {
			t.Errorf("%s: no problem reported", name)
		}
	}
	for name, cfg := range map[string]Config{"no access": {Label: "t1"}, "refused": {Label: "t1", Logs: failing{}}} {
		r, problems := checkLogs(context.Background(), cfg, time.Now(), cases, subject)
		if r.verified || len(problems) != 0 || r.reason == "" {
			t.Errorf("%s: %+v, %v; want not verified without failing", name, r, problems)
		}
	}
}
