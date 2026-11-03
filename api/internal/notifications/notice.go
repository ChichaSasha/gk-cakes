// Package notifications sends transactional email (K08) through Resend.
//
// A domain transaction enqueues a notice together with its business change
// (business.notification_enqueue: intent, job and audit event commit
// together). The worker's Dispatcher sends it outside any database
// transaction under the K11 recovery coordinator: the encrypted payload is in
// the independent journal before the provider call, every replay sends the
// identical request with the same idempotency key, and an outcome that stays
// unknown past the provider's idempotency window goes to manager
// reconciliation instead of a new send. Only provider evidence is recorded;
// inbox delivery or reading is never claimed.
package notifications

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The approved sender (K08). Payloads with any other sender are refused.
const (
	From     = "GK Cakes <orders@gkcake.com>"
	ReplyTo  = "gk.cake.london@gmail.com"
	Provider = "resend"
)

// Limits on a message. The subject limit and CR/LF rule follow K08; the body
// limits bound the stored payload (database limit 256 KiB).
const (
	MaxSubjectRunes = 200
	MaxTextBytes    = 64 << 10
	MaxHTMLBytes    = 128 << 10
	maxPayloadBytes = 256 << 10
	maxAddressBytes = 254
	intentTagName   = "intent"
)

// Message is one transactional email to one recipient.
type Message struct {
	To      string // a bare address, frozen when the notice is enqueued
	Subject string
	Text    string
	HTML    string // optional
}

// Notice is a notification intent ready to be enqueued with its transition.
type Notice struct {
	IntentID         string
	TransitionID     string
	RecipientID      string
	TemplateRevision string
	IdempotencyKey   string
	// Payload is the exact dispatch: provider, environment, idempotency key
	// and the provider request. It is stored once and sent byte-identically.
	Payload json.RawMessage
}

// Tag is a Resend email tag.
type Tag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Request is the Resend send-email request body. It is a frozen wire
// format: ParseDispatch requires stored bytes to equal its encoding, so
// reordering fields or adding one without omitempty would refuse every stored
// but unsent notice. Change it only with a payload version.
type Request struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	ReplyTo []string `json:"reply_to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html,omitempty"`
	Tags    []Tag    `json:"tags"`
}

// payload is the stored dispatch. Request keeps its exact bytes.
type payload struct {
	Provider       string          `json:"provider"`
	Environment    string          `json:"environment"`
	IdempotencyKey string          `json:"idempotency_key"`
	Request        json.RawMessage `json:"request"`
}

var (
	uuidPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	revisionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	envPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

// NewNotice builds the notice for msg in environment env with fresh intent
// and idempotency identifiers. The recipient address and content are frozen
// into the payload: later profile changes never retarget it.
func NewNotice(env, transitionID, recipientID, templateRevision string, msg Message) (Notice, error) {
	switch {
	case !envPattern.MatchString(env):
		return Notice{}, errors.New("notifications: environment must be a lower-case name")
	case !uuidPattern.MatchString(transitionID) || !uuidPattern.MatchString(recipientID):
		return Notice{}, errors.New("notifications: transition and recipient IDs must be lower-case UUIDs")
	case !revisionPattern.MatchString(templateRevision):
		return Notice{}, errors.New("notifications: template revision must match [a-z0-9][a-z0-9._-]{0,63}")
	}
	if err := validateMessage(msg); err != nil {
		return Notice{}, err
	}
	n := Notice{IntentID: newUUID(), TransitionID: transitionID, RecipientID: recipientID,
		TemplateRevision: templateRevision, IdempotencyKey: newUUID()}
	req := Request{From: From, To: []string{msg.To}, ReplyTo: []string{ReplyTo}, Subject: msg.Subject,
		Text: msg.Text, HTML: msg.HTML, Tags: []Tag{{Name: intentTagName, Value: n.IntentID}}}
	reqBytes, err := marshal(req)
	if err != nil {
		return Notice{}, err
	}
	p, err := marshal(payload{Provider: Provider, Environment: env, IdempotencyKey: n.IdempotencyKey, Request: reqBytes})
	if err != nil {
		return Notice{}, err
	}
	if len(p) > maxPayloadBytes {
		return Notice{}, fmt.Errorf("notifications: payload is %d bytes, more than %d", len(p), maxPayloadBytes)
	}
	n.Payload = p
	return n, nil
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func validateMessage(msg Message) error {
	for _, s := range []string{msg.To, msg.Subject, msg.Text, msg.HTML} {
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return errors.New("notifications: message fields must be UTF-8 without NUL")
		}
	}
	switch {
	case !bareAddress(msg.To):
		return errors.New("notifications: recipient must be one bare email address")
	case strings.TrimSpace(msg.Subject) == "" || utf8.RuneCountInString(msg.Subject) > MaxSubjectRunes:
		return fmt.Errorf("notifications: subject must be 1 to %d characters", MaxSubjectRunes)
	case strings.ContainsAny(msg.Subject, "\r\n"):
		return errors.New("notifications: subject must not contain CR or LF")
	case strings.TrimSpace(msg.Text) == "" || len(msg.Text) > MaxTextBytes:
		return fmt.Errorf("notifications: text body must be 1 to %d bytes", MaxTextBytes)
	case len(msg.HTML) > MaxHTMLBytes:
		return fmt.Errorf("notifications: HTML body must be at most %d bytes", MaxHTMLBytes)
	}
	return nil
}

func bareAddress(s string) bool {
	if s == "" || len(s) > maxAddressBytes || strings.ContainsAny(s, "\r\n<>\"(),; \t") {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Name == "" && addr.Address == s && strings.Contains(s[strings.LastIndexByte(s, '@')+1:], ".")
}

// Dispatch is a stored payload checked for sending.
type Dispatch struct {
	IdempotencyKey string
	// Request is the exact provider request body.
	Request []byte
}

// ParseDispatch checks a stored payload before it is sent: it must be this
// environment's, carry this intent's key and tag, and use the approved
// sender and a single recipient. Request keeps the stored bytes exactly.
func ParseDispatch(stored []byte, env, intentID, key string) (Dispatch, error) {
	var p payload
	if err := json.Unmarshal(stored, &p); err != nil {
		return Dispatch{}, errors.New("notifications: stored payload is not a dispatch")
	}
	switch {
	case p.Provider != Provider:
		return Dispatch{}, fmt.Errorf("notifications: stored payload is for provider %q", p.Provider)
	case p.Environment != env:
		return Dispatch{}, fmt.Errorf("notifications: stored payload belongs to environment %q, not %q", p.Environment, env)
	case p.IdempotencyKey != key:
		return Dispatch{}, errors.New("notifications: stored payload carries another idempotency key")
	}
	var req Request
	dec := json.NewDecoder(bytes.NewReader(p.Request))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return Dispatch{}, errors.New("notifications: stored request is not a supported send request")
	}
	switch {
	case req.From != From || len(req.ReplyTo) != 1 || req.ReplyTo[0] != ReplyTo:
		return Dispatch{}, errors.New("notifications: stored request does not use the approved sender")
	case len(req.To) != 1:
		return Dispatch{}, errors.New("notifications: stored request must have exactly one recipient")
	case len(req.Tags) != 1 || req.Tags[0] != (Tag{Name: intentTagName, Value: intentID}):
		return Dispatch{}, errors.New("notifications: stored request is not tagged with its intent")
	}
	if err := validateMessage(Message{To: req.To[0], Subject: req.Subject, Text: req.Text, HTML: req.HTML}); err != nil {
		return Dispatch{}, err
	}
	// The stored bytes must be exactly the canonical encoding of what was
	// checked: no duplicate or case-variant keys the provider might read
	// differently.
	if canonical, err := marshal(req); err != nil || !bytes.Equal(canonical, p.Request) {
		return Dispatch{}, errors.New("notifications: stored request is not in canonical form")
	}
	return Dispatch{IdempotencyKey: key, Request: p.Request}, nil
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
