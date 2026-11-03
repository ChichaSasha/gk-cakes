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

func (l lines) Since(context.Context, time.Time) ([]string, bool, error) { return l, true, nil }

type truncated []string

func (l truncated) Since(context.Context, time.Time) ([]string, bool, error) { return l, false, nil }

type failing struct{}

func (failing) Since(context.Context, time.Time) ([]string, bool, error) {
	return nil, false, errors.New("Not Authorized")
}

const (
	intentA = "11111111-1111-4111-8111-111111111111"
	intentB = "22222222-2222-4222-8222-222222222222"
)

func scanFixture() ([]*Case, []Probe, string) {
	cases := []*Case{{To: "delivered+t1@resend.dev"}, {To: "owner@example.org"}}
	cases[0].Notice.IntentID, cases[1].Notice.IntentID = intentA, intentB
	probes := []Probe{
		{Name: "unsigned", Reason: "missing or malformed signature headers"},
		{Name: "signed with another secret", Reason: "no matching signature", SvixID: "msg_probea"},
		{Name: "stale timestamp", Reason: "timestamp outside the tolerance", SvixID: "msg_probeb"},
		{Name: "oversized"},
	}
	return cases, probes, "GK Cakes sandbox proof t1"
}

// Railway shows structured lines as the message followed by attributes.
var cleanRun = lines{
	"email provider call result=accepted job_id=j intent_id=" + intentA,
	"email webhook event result=recorded intent_id=" + intentA,
	"email webhook refused reason=missing or malformed signature headers svix_id=",
	"email webhook refused reason=no matching signature svix_id=msg_probea",
	"email webhook refused reason=timestamp outside the tolerance svix_id=msg_probeb",
	"request method=POST path=/webhooks/email status=413",
}

func TestLogScanNeedsThisRunAndNoPersonalData(t *testing.T) {
	cases, probes, subject := scanFixture()
	if r, problems := checkLogs(context.Background(), Config{Label: "t1", Logs: cleanRun}, time.Now(), cases, probes, subject); len(problems) != 0 || !r.verified {
		t.Fatalf("clean logs: %+v, %v", r, problems)
	}
	// The same lines as raw JSON (if Railway does not split attributes).
	raw := lines{
		`{"level":"INFO","msg":"email provider call","result":"accepted","intent_id":"` + intentA + `"}`,
		`{"level":"INFO","msg":"email webhook event","result":"recorded","intent_id":"` + intentA + `"}`,
		`{"level":"WARN","msg":"email webhook refused","reason":"missing or malformed signature headers","svix_id":""}`,
		`{"level":"WARN","msg":"email webhook refused","reason":"no matching signature","svix_id":"msg_probea"}`,
		`{"level":"WARN","msg":"email webhook refused","reason":"timestamp outside the tolerance","svix_id":"msg_probeb"}`,
		`{"level":"INFO","msg":"request","method":"POST","path":"/webhooks/email","status":413,"duration_ms":1}`,
	}
	if _, problems := checkLogs(context.Background(), Config{Label: "t1", Logs: raw}, time.Now(), cases, probes, subject); len(problems) != 0 {
		t.Fatalf("raw JSON logs: %v", problems)
	}
	without := func(drop int) lines {
		out := append(lines{}, cleanRun[:drop]...)
		return append(out, cleanRun[drop+1:]...)
	}
	for name, l := range map[string]lines{
		"recipient":                        append(lines{"debug to=Delivered+T1@resend.dev"}, cleanRun...),
		"local part":                       append(lines{"debug owner"}, cleanRun...),
		"subject":                          append(lines{"subject=GK Cakes sandbox proof t1"}, cleanRun...),
		"body (run label)":                 append(lines{"body=Controlled GK-024 sandbox proof message t1."}, cleanRun...),
		"no dispatch line":                 without(0),
		"no recorded webhook line":         without(1),
		"another run's dispatch only":      append(lines{"email provider call intent_id=99999999-9999-4999-8999-999999999999"}, without(0)...),
		"an ignored event only":            append(lines{"email webhook event ignored intent_id=" + intentA}, without(1)...),
		"unsigned refusal not logged":      without(2),
		"forged refusal not logged":        without(3),
		"stale refused for another reason": append(lines{"email webhook refused reason=no matching signature svix_id=msg_probeb"}, without(4)...),
		"oversized not logged":             without(5),
		"empty":                            {},
	} {
		if _, problems := checkLogs(context.Background(), Config{Label: "t1", Logs: l}, time.Now(), cases, probes, subject); len(problems) == 0 {
			t.Errorf("%s: no problem reported", name)
		}
	}
	for name, cfg := range map[string]Config{
		"no access": {Label: "t1"}, "refused": {Label: "t1", Logs: failing{}}, "truncated": {Label: "t1", Logs: truncated(cleanRun)},
	} {
		r, problems := checkLogs(context.Background(), cfg, time.Now(), cases, probes, subject)
		if r.verified || len(problems) != 0 || r.reason == "" {
			t.Errorf("%s: %+v, %v; want not verified (incomplete), not a failure", name, r, problems)
		}
	}
}
