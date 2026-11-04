package payments

import (
	"time"
)

// Verdicts of a reconcile, recorded by business.payment_reconcile_record.
const (
	VerdictPaid      = "paid"
	VerdictPending   = "pending"
	VerdictCancelled = "cancelled"
	VerdictMismatch  = "mismatch"
)

// Expectation is what a retrieved session must match.
type Expectation struct {
	CollectionID string
	OrderID      string
	Environment  string
	SessionID    string // the session that was retrieved
	AmountMinor  int64
	Livemode     bool
}

// Evidence is the bounded record of what Stripe showed: IDs, statuses,
// amounts, currencies and times only; never card or customer details.
type Evidence struct {
	SessionID       string `json:"session_id"`
	SessionStatus   string `json:"session_status,omitempty"`
	PaymentStatus   string `json:"payment_status,omitempty"`
	AmountTotal     *int64 `json:"amount_total,omitempty"`
	Currency        string `json:"currency,omitempty"`
	Livemode        bool   `json:"livemode"`
	PaymentIntentID string `json:"payment_intent_id,omitempty"`
	IntentStatus    string `json:"payment_intent_status,omitempty"`
	AmountReceived  *int64 `json:"amount_received,omitempty"`
	IntentCurrency  string `json:"payment_intent_currency,omitempty"`
	ChargeID        string `json:"charge_id,omitempty"`
	ChargeStatus    string `json:"charge_status,omitempty"`
	ChargePaid      *bool  `json:"charge_paid,omitempty"`
	ChargeCaptured  *bool  `json:"charge_captured,omitempty"`
	AmountCaptured  *int64 `json:"amount_captured,omitempty"`
	ChargeCurrency  string `json:"charge_currency,omitempty"`
	ChargeCreated   string `json:"charge_created,omitempty"`
}

// Verdict is the decision about one retrieved session.
type Verdict struct {
	Kind      string
	Reason    string    // mismatch only; no personal data
	SuccessAt time.Time // paid only: the charge's provider time
	Evidence  Evidence
}

func evidenceOf(s *Session) Evidence {
	ev := Evidence{SessionID: sanitize(sessionIDPattern, s.ID), SessionStatus: sanitize(statusPattern, s.Status),
		PaymentStatus: sanitize(statusPattern, s.PaymentStatus), AmountTotal: s.AmountTotal,
		Currency: sanitize(statusPattern, s.Currency), Livemode: s.Livemode}
	if pi := s.PaymentIntent; pi != nil {
		ev.PaymentIntentID = sanitize(piIDPattern, pi.ID)
		if pi.Expanded {
			ev.IntentStatus = sanitize(statusPattern, pi.Status)
			ev.AmountReceived = &pi.AmountReceived
			ev.IntentCurrency = sanitize(statusPattern, pi.Currency)
			if ch := pi.LatestCharge; ch != nil {
				ev.ChargeID = sanitize(objectIDPattern, ch.ID)
				if ch.Expanded {
					ev.ChargeStatus = sanitize(statusPattern, ch.Status)
					ev.ChargePaid, ev.ChargeCaptured = &ch.Paid, &ch.Captured
					ev.AmountCaptured = &ch.AmountCaptured
					ev.ChargeCurrency = sanitize(statusPattern, ch.Currency)
					if ch.Created > 0 {
						ev.ChargeCreated = time.Unix(ch.Created, 0).UTC().Format(time.RFC3339)
					}
				}
			}
		}
	}
	return ev
}

// Evaluate decides what a retrieved session proves (K04): a session whose
// identity, metadata, mode, currency or amount does not match the
// collection is a mismatch (never confirmed); it is paid only when the
// session is paid AND its payment intent succeeded with the full amount in
// GBP AND the latest charge is paid, captured and succeeded for the full
// amount (the success instant is the charge's provider time); a session
// that expired without a succeeded payment is cancelled; anything else
// (open, processing, requires_capture, unpaid, no_payment_required on a
// nonzero amount, complete without a succeeded payment intent) is pending.
func Evaluate(s *Session, exp Expectation) Verdict {
	v := Verdict{Evidence: evidenceOf(s)}
	mismatch := func(reason string) Verdict {
		v.Kind, v.Reason = VerdictMismatch, reason
		return v
	}
	switch {
	case s.ID != exp.SessionID:
		return mismatch("Stripe returned another session")
	case s.Metadata["collection_id"] != exp.CollectionID || s.Metadata["order_id"] != exp.OrderID ||
		s.Metadata["environment"] != exp.Environment || s.ClientReferenceID != exp.OrderID:
		return mismatch("session metadata does not name this collection, order and environment")
	case s.Livemode != exp.Livemode:
		return mismatch("session is in the wrong Stripe mode")
	case s.Currency != "gbp":
		return mismatch("session currency is not GBP")
	case s.AmountTotal == nil || *s.AmountTotal != exp.AmountMinor:
		return mismatch("session amount does not match the collection")
	}
	pi := s.PaymentIntent
	var ch *Charge
	if pi != nil && pi.Expanded {
		switch {
		case !piIDPattern.MatchString(pi.ID):
			return mismatch("payment intent has no valid ID")
		case pi.Metadata["collection_id"] != "" && pi.Metadata["collection_id"] != exp.CollectionID,
			pi.Metadata["order_id"] != "" && pi.Metadata["order_id"] != exp.OrderID,
			pi.Metadata["environment"] != "" && pi.Metadata["environment"] != exp.Environment:
			return mismatch("payment intent metadata names another collection")
		case pi.Livemode != exp.Livemode:
			return mismatch("payment intent is in the wrong Stripe mode")
		case pi.Currency != "gbp":
			return mismatch("payment intent currency is not GBP")
		case pi.Status == "succeeded" && pi.AmountReceived != exp.AmountMinor:
			return mismatch("payment intent received another amount")
		}
		ch = pi.LatestCharge
		if ch != nil && ch.Expanded {
			switch {
			case ch.Livemode != exp.Livemode:
				return mismatch("charge is in the wrong Stripe mode")
			case ch.Currency != "gbp":
				return mismatch("charge currency is not GBP")
			case ch.Status == "succeeded" && ch.Captured && ch.AmountCaptured != exp.AmountMinor:
				return mismatch("charge captured another amount")
			}
		}
	}
	paid := s.PaymentStatus == "paid" && pi != nil && pi.Expanded && pi.Status == "succeeded" &&
		pi.AmountReceived == exp.AmountMinor && pi.Currency == "gbp" &&
		ch != nil && ch.Expanded && ch.Paid && ch.Captured && ch.Status == "succeeded" &&
		ch.AmountCaptured == exp.AmountMinor && ch.Created > 0
	switch {
	case paid:
		v.Kind, v.SuccessAt = VerdictPaid, time.Unix(ch.Created, 0).UTC()
	case s.Status == "expired" && pi != nil && pi.Expanded && pi.Status == "succeeded":
		return mismatch("expired session with a succeeded payment intent")
	case s.Status == "expired":
		v.Kind = VerdictCancelled
	default:
		v.Kind = VerdictPending
	}
	return v
}
