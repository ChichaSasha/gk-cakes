// Package emailproof runs the GK-024 AC3 sandbox proof against a deployed
// environment: it enqueues controlled notices (Resend test recipients, and
// optionally the business mailbox for the display-name check) exactly as a
// domain transaction would, waits for the worker to send them and the signed
// webhook to record delivery or bounce evidence, checks that the public
// webhook endpoint refuses unsigned, forged and stale deliveries, and scans the
// environment's logs for the recipients and subject. It reports IDs, states
// and counts only.
package emailproof

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/notifications"
	"github.com/jackc/pgx/v5"
)

// TemplateRevision marks proof notices.
const TemplateRevision = "sandbox-proof.v1"

// LogSearcher returns the environment's log lines since a time (message and
// attributes as text); complete is false when older lines may be missing.
type LogSearcher interface {
	Since(ctx context.Context, since time.Time) (lines []string, complete bool, err error)
}

// ErrIncomplete means no evidence contradicted AC3 but part of it could not
// be checked (logs not readable or truncated).
var ErrIncomplete = errors.New("emailproof: incomplete")

// Config configures a proof run.
type Config struct {
	DB             *pgx.Conn // migration credential; may SET ROLE gk_owner
	Environment    string    // APP_ENV of the target, e.g. "staging"
	APIURL         string    // https://<api host>, no trailing slash
	HTTP           *http.Client
	Logs           LogSearcher   // nil: the log check is not made (incomplete unless SkipLogs)
	SkipLogs       bool          // explicitly accept a run without the log check
	LogDelay       time.Duration // wait for log indexing before each log scan
	Label          string        // unique per run, [a-z0-9]{1,24}
	ExtraRecipient string        // optional controlled mailbox (display-name check)
	Wait           time.Duration
	Poll           time.Duration
	Out            io.Writer // Markdown report
	Now            func() time.Time
}

var labelPattern = regexp.MustCompile(`^[a-z0-9]{1,24}$`)

// Case is one controlled notice and the evidence it must reach.
type Case struct {
	Name      string
	To        string
	Want      []string // acceptable final intent states
	Notice    notifications.Notice
	State     string
	MessageID string
	Attempts  []string
	Events    []string
	Recovery  string
	Job       string
}

// Probe is one refused webhook delivery. Reason is the refusal the API must
// log for it (matched by svix-id where one is sent), or "" when the refusal
// happens before the webhook code (oversized: the access log's 413).
type Probe struct {
	Name   string
	Status int
	Want   int
	SvixID string
	Reason string
	Logged bool
}

func (c Config) validate() error {
	switch {
	case c.DB == nil || c.HTTP == nil || c.Out == nil:
		return errors.New("emailproof: database, HTTP client and output are required")
	case !labelPattern.MatchString(c.Label):
		return errors.New("emailproof: label must be 1 to 24 lower-case letters or digits")
	case !strings.HasPrefix(c.APIURL, "https://") && !strings.HasPrefix(c.APIURL, "http://"):
		return errors.New("emailproof: API URL must be http(s)://host")
	case c.Wait <= 0 || c.Poll <= 0:
		return errors.New("emailproof: wait and poll must be positive")
	}
	if c.ExtraRecipient != "" {
		if a, err := mail.ParseAddress(c.ExtraRecipient); err != nil || a.Address != c.ExtraRecipient {
			return errors.New("emailproof: extra recipient must be one bare address")
		}
	}
	return nil
}

// Run performs the proof and writes the report. It returns an error when any
// expected evidence is missing, a probe was not refused or personal data was
// found in the logs.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	started := cfg.Now()
	subject := "GK Cakes sandbox proof " + cfg.Label
	cases := []*Case{
		{Name: "delivered", To: "delivered+" + cfg.Label + "@resend.dev", Want: []string{"delivered_to_server"}},
		{Name: "bounced", To: "bounced+" + cfg.Label + "@resend.dev", Want: []string{"bounced"}},
	}
	if cfg.ExtraRecipient != "" {
		cases = append(cases, &Case{Name: "display name (business mailbox)", To: cfg.ExtraRecipient,
			Want: []string{"accepted", "delivered_to_server"}})
	}

	var problems []string
	frozen, err := ownerBool(ctx, cfg.DB, "SELECT business.recovery_frozen()")
	if err != nil {
		return fmt.Errorf("emailproof: freeze check: %w", err)
	}
	if frozen {
		return errors.New("emailproof: outgoing effects are frozen (restore in progress); nothing was sent")
	}

	probes, err := probeEndpoint(ctx, cfg)
	if err != nil {
		return err
	}
	problems = append(problems, probeProblems(probes)...)

	for _, c := range cases {
		n, err := notifications.NewNotice(cfg.Environment, newUUID(), newUUID(), TemplateRevision, notifications.Message{
			To: c.To, Subject: subject,
			Text: "Controlled GK-024 sandbox proof message " + cfg.Label + ". No customer order or payment is involved.",
		})
		if err != nil {
			return fmt.Errorf("emailproof: notice %s: %w", c.Name, err)
		}
		if err := ownerTx(ctx, cfg.DB, func(tx pgx.Tx) error { return notifications.Enqueue(ctx, tx, n, "system", "") }); err != nil {
			return fmt.Errorf("emailproof: enqueue %s: %w", c.Name, err)
		}
		c.Notice = n
	}

	deadline := cfg.Now().Add(cfg.Wait)
	for {
		done := true
		for _, c := range cases {
			if err := observe(ctx, cfg.DB, c); err != nil {
				return err
			}
			if !slices.Contains(c.Want, c.State) {
				done = false
			}
		}
		if done || cfg.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.Poll):
		}
	}
	for _, c := range cases {
		if !slices.Contains(c.Want, c.State) {
			problems = append(problems, fmt.Sprintf("%s: intent is %q after %s, want %s", c.Name, c.State, cfg.Wait, strings.Join(c.Want, " or ")))
		}
	}
	var stored int
	if err := ownerTx(ctx, cfg.DB, func(tx pgx.Tx) error {
		ids := make([]string, 0, len(probes))
		for _, p := range probes {
			ids = append(ids, p.SvixID)
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM business.email_event WHERE svix_id = ANY($1)", ids).Scan(&stored)
	}); err != nil {
		return fmt.Errorf("emailproof: probe events: %w", err)
	}
	if stored != 0 {
		problems = append(problems, fmt.Sprintf("%d refused webhook probes were stored", stored))
	}

	logReport, logProblems := checkLogs(ctx, cfg, started, cases, probes, subject)
	problems = append(problems, logProblems...)
	incomplete := !logReport.verified && !cfg.SkipLogs
	writeReport(cfg, cases, probes, stored, logReport, problems, incomplete)
	switch {
	case len(problems) > 0:
		return fmt.Errorf("emailproof: %d problem(s): %s", len(problems), strings.Join(problems, "; "))
	case incomplete:
		return fmt.Errorf("%w: %s", ErrIncomplete, logReport.reason)
	}
	return nil
}

func ownerTx(ctx context.Context, db *pgx.Conn, fn func(pgx.Tx) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE gk_owner"); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ownerBool(ctx context.Context, db *pgx.Conn, sql string) (bool, error) {
	var v bool
	err := ownerTx(ctx, db, func(tx pgx.Tx) error { return tx.QueryRow(ctx, sql).Scan(&v) })
	return v, err
}

func observe(ctx context.Context, db *pgx.Conn, c *Case) error {
	return ownerTx(ctx, db, func(tx pgx.Tx) error {
		var msg, recovery, job *string
		err := tx.QueryRow(ctx, `SELECT i.state, i.provider_message_id,
			coalesce((SELECT array_agg(a.result ORDER BY a.attempt_no) FROM business.notification_attempt a WHERE a.intent_id = i.id), '{}'),
			coalesce((SELECT array_agg(e.event_type || ':' || e.correlation || ':' || coalesce(e.applied_state, '-') ORDER BY e.received_at)
			          FROM business.email_event e WHERE e.intent_id = i.id), '{}'),
			(SELECT r.state FROM business.recovery_operation r WHERE r.id = i.idempotency_key),
			(SELECT j.state FROM business.job j WHERE j.id = i.job_id)
			FROM business.notification_intent i WHERE i.id = $1::uuid`, c.Notice.IntentID).
			Scan(&c.State, &msg, &c.Attempts, &c.Events, &recovery, &job)
		c.MessageID, c.Recovery, c.Job = deref(msg), deref(recovery), deref(job)
		return err
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// probeEndpoint sends deliveries the endpoint must refuse: unsigned, signed
// with another secret, stale, and oversized.
func probeEndpoint(ctx context.Context, cfg Config) ([]Probe, error) {
	forged := make([]byte, 32)
	_, _ = rand.Read(forged)
	body := []byte(`{"type":"email.delivered","created_at":"2026-01-01T00:00:00Z","data":{"email_id":"proof-forged","tags":{"env":"` + cfg.Environment + `"}}}`)
	now := cfg.Now()
	// The stale probe is refused for its timestamp before any signature is
	// checked; the log reason tells the two apart.
	probes := []Probe{
		{Name: "unsigned", Want: http.StatusUnauthorized, Reason: "missing or malformed signature headers"},
		{Name: "signed with another secret", Want: http.StatusUnauthorized, Reason: "no matching signature"},
		{Name: "stale timestamp", Want: http.StatusUnauthorized, Reason: "timestamp outside the tolerance"},
		{Name: "oversized", Want: http.StatusRequestEntityTooLarge},
	}
	for i := range probes {
		p := &probes[i]
		if p.Name != "unsigned" {
			p.SvixID = "msg_probe" + randomHex(8) // never the run label (the log scan searches for it)
		}
		h := http.Header{}
		payload := body
		switch p.Name {
		case "signed with another secret":
			sign(h, forged, p.SvixID, now, payload)
		case "stale timestamp":
			sign(h, forged, p.SvixID, now.Add(-time.Hour), payload)
		case "oversized":
			payload = bytes.Repeat([]byte(" "), notifications.MaxWebhookBytes+1)
			sign(h, forged, p.SvixID, now, payload)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIURL+"/webhooks/email", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		for k, v := range h {
			req.Header[k] = v
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := cfg.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("emailproof: webhook probe %q: %w", p.Name, err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		p.Status = resp.StatusCode
		if p.Name == "unsigned" && p.Status == http.StatusNotFound {
			return nil, errors.New("emailproof: /webhooks/email is not served: EMAIL_WEBHOOK_SECRET is not set on the API")
		}
	}
	return probes, nil
}

func probeProblems(probes []Probe) []string {
	var problems []string
	for _, p := range probes {
		if p.Status != p.Want {
			problems = append(problems, fmt.Sprintf("webhook probe %q answered %d, want %d", p.Name, p.Status, p.Want))
		}
	}
	return problems
}

func sign(h http.Header, key []byte, id string, at time.Time, body []byte) {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	h.Set("svix-id", id)
	h.Set("svix-timestamp", ts)
	h.Set("svix-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

type logReport struct {
	verified   bool
	reason     string
	lines      int
	controls   map[string]int
	sensitives int
}

const (
	controlDispatch = "email provider call"
	controlWebhook  = "email webhook event"
)

// checkLogs scans the environment's logs since the run started. The run's
// recipients, their local parts, the subject and the run label (which is also
// in the body) must not appear. This run's dispatch and recorded webhook
// lines (carrying its intent IDs) and each probe's refusal must, so an empty,
// unrelated or partial log cannot pass. Lines may take a while to be indexed:
// the scan waits LogDelay and retries a few times while controls are missing.
func checkLogs(ctx context.Context, cfg Config, since time.Time, cases []*Case, probes []Probe, subject string) (logReport, []string) {
	r := logReport{}
	if cfg.Logs == nil {
		r.reason = "no log access configured"
		return r, nil
	}
	needles := []string{subject, cfg.Label}
	for _, c := range cases {
		needles = append(needles, c.To, strings.SplitN(c.To, "@", 2)[0])
	}
	var problems []string
	for try := 0; try < 3; try++ {
		if cfg.LogDelay > 0 {
			select {
			case <-ctx.Done():
				r.reason = ctx.Err().Error()
				return r, nil
			case <-time.After(cfg.LogDelay):
			}
		}
		lines, complete, err := cfg.Logs.Since(ctx, since.Add(-time.Minute))
		switch {
		case err != nil:
			r = logReport{reason: "logs not available: " + err.Error()}
			return r, nil
		case !complete:
			r = logReport{reason: fmt.Sprintf("log window truncated at %d lines", len(lines))}
			return r, nil
		}
		r = scanLines(lines, needles, cases, probes)
		problems = nil
		if r.sensitives > 0 {
			problems = append(problems, fmt.Sprintf("%d log line(s) contain a proof recipient, the subject or the run label", r.sensitives))
		}
		missing := false
		for _, k := range []string{controlDispatch, controlWebhook} {
			if r.controls[k] == 0 {
				missing = true
				problems = append(problems, fmt.Sprintf("no %q log line for this run's intents: the log scan did not see the run", k))
			}
		}
		for _, p := range probes {
			if !p.Logged {
				missing = true
				problems = append(problems, fmt.Sprintf("webhook probe %q: no log line shows the expected refusal", p.Name))
			}
		}
		if !missing || r.sensitives > 0 {
			break
		}
	}
	return r, problems
}

// scanLines counts sensitive lines, this run's control lines, and marks the
// probes whose refusal is logged.
func scanLines(lines, needles []string, cases []*Case, probes []Probe) logReport {
	r := logReport{verified: true, lines: len(lines), controls: map[string]int{controlDispatch: 0, controlWebhook: 0}}
	for i := range probes {
		probes[i].Logged = false
	}
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, n := range needles {
			if strings.Contains(lower, strings.ToLower(n)) {
				r.sensitives++
				break
			}
		}
		for _, c := range cases {
			if c.Notice.IntentID == "" || !strings.Contains(line, c.Notice.IntentID) {
				continue
			}
			if hasMessage(line, controlDispatch) {
				r.controls[controlDispatch]++
			}
			if hasMessage(line, controlWebhook) && hasField(line, "result", "recorded") {
				r.controls[controlWebhook]++
			}
		}
		for i := range probes {
			p := &probes[i]
			switch {
			case p.Reason == "":
				p.Logged = p.Logged || (hasMessage(line, "request") && hasField(line, "path", "/webhooks/email") && hasField(line, "status", "413"))
			case hasMessage(line, "email webhook refused") && hasField(line, "reason", p.Reason) &&
				(p.SvixID == "" || hasField(line, "svix_id", p.SvixID)):
				p.Logged = true
			}
		}
	}
	return r
}

// hasMessage matches a log line's message, whether Railway shows it as the
// message followed by attributes or as the raw JSON line.
func hasMessage(line, msg string) bool {
	return line == msg || strings.HasPrefix(line, msg+" ") ||
		strings.Contains(line, `"msg":"`+msg+`"`) || strings.Contains(line, `"message":"`+msg+`"`)
}

// hasField matches an attribute in any representation: key=value,
// key="value" (a JSON-encoded attribute value), or a JSON string or number in
// a raw JSON line.
func hasField(line, key, value string) bool {
	quoted, _ := json.Marshal(value)
	return strings.Contains(line, " "+key+"="+value) || strings.Contains(line, " "+key+"="+string(quoted)) ||
		strings.Contains(line, `"`+key+`":`+string(quoted)) ||
		strings.Contains(line, `"`+key+`":`+value+`,`) || strings.Contains(line, `"`+key+`":`+value+`}`)
}

func writeReport(cfg Config, cases []*Case, probes []Probe, stored int, logs logReport, problems []string, incomplete bool) {
	w := cfg.Out
	fmt.Fprintf(w, "### GK-024 sandbox proof `%s` (%s)\n\n", cfg.Label, cfg.Environment)
	fmt.Fprintln(w, "| Case | Intent | State | Provider message | Attempts | Webhook events | Recovery | Job |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|")
	for _, c := range cases {
		fmt.Fprintf(w, "| %s | `%s` | **%s** | `%s` | %s | %s | %s | %s |\n", c.Name, c.Notice.IntentID, c.State, c.MessageID,
			strings.Join(c.Attempts, ", "), strings.Join(c.Events, "<br>"), c.Recovery, c.Job)
	}
	fmt.Fprintln(w, "\n| Webhook probe | Status | Expected | Refusal logged |")
	fmt.Fprintln(w, "|---|---|---|---|")
	for _, p := range probes {
		logged := "not checked"
		if logs.verified {
			logged = map[bool]string{true: "yes", false: "**no**"}[p.Logged]
		}
		fmt.Fprintf(w, "| %s | %d | %d | %s |\n", p.Name, p.Status, p.Want, logged)
	}
	fmt.Fprintf(w, "\nRefused probes stored: %d.\n\n", stored)
	if logs.verified {
		fmt.Fprintf(w, "Logs: %d line(s) scanned; %d with a proof recipient, the subject or the run label; this run's dispatch lines %d, recorded webhook lines %d.\n\n",
			logs.lines, logs.sensitives, logs.controls[controlDispatch], logs.controls[controlWebhook])
	} else {
		fmt.Fprintf(w, "Logs: **not verified** (%s).\n\n", logs.reason)
	}
	switch {
	case len(problems) == 0 && incomplete:
		fmt.Fprintf(w, "Result: **incomplete**: the log checks could not be completed (%s).\n", logs.reason)
		return
	case len(problems) == 0:
		fmt.Fprintln(w, "Result: **pass**.")
		return
	}
	fmt.Fprintln(w, "Result: **fail**")
	for _, p := range problems {
		fmt.Fprintf(w, "- %s\n", p)
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
