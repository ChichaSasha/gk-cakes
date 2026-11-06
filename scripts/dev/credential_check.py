#!/usr/bin/env python3
"""Refuse text that looks like it carries a credential (before it goes to
Trello, a PR or a report). Values that are exactly REDACTED and
Idempotency-Key values are allowed. Errs on the side of refusing.

  credential_check.py <file>       exit 1 and name the reason if one is found
  credential_check.py --self-test
"""
import re
import sys

TOKEN_FORMATS = [
    ("private key", r"-----BEGIN [A-Z ]*PRIVATE KEY-----"),
    ("Trello token", r"\bATTA[0-9a-fA-F]{20,}"),
    ("GitHub token", r"\bgh[pousr]_[A-Za-z0-9]{20,}|\bgithub_pat_\w{20,}"),
    ("Stripe key", r"\b[sr]k_(?:live|test)_\w{10,}|\bwhsec_\w{10,}"),
    ("Supabase secret key", r"\bsb_secret_\w{10,}"),
    ("Resend key", r"\bre_[A-Za-z0-9]{20,}"),
    ("AWS key", r"\bAKIA[0-9A-Z]{16}\b"),
    ("Slack token", r"\bxox[abprs]-[\w-]{10,}"),
    ("JWT", r"\beyJ[\w-]{10,}\.eyJ[\w-]{10,}"),
    ("bearer token", r"(?i)\bbearer\s+(?!REDACTED\b)[\w.~+/=-]{12,}"),
    ("basic credentials", r"(?i)\bbasic\s+(?!REDACTED\b)[A-Za-z0-9+/]{8,}={0,2}"),
    ("password in a URL", r"(?i)\b[a-z][a-z0-9+.-]*://[^/\s:@]+:(?!REDACTED@)[^\s@]+@"),
    ("key in a URL (DSN)", r"(?i)\bhttps?://[0-9a-f]{16,}@"),
]
# NAME = value / NAME: value, where NAME is a secret word, alone or as a
# _/- separated part (access_token, client_secret, STAFF_PW, DB_PASS,
# secret_key_base, Idempotency-Key ...). Quoted values are read to the closing
# quote. Bare "key" needs a random-looking value ("Primary key: id" is prose).
SECRET_WORD = r"(?:api[_-]?key|apikey|key|token|secret|password|passwd|pwd|pw|pass|credentials?|cookie|dsn)"
NAMED = re.compile(
    r"(?i)(?<![\w-])(?P<name>(?:[A-Za-z0-9]+[_-])*" + SECRET_WORD + r"(?:[_-][A-Za-z0-9]+)*)[\"']?\s*[=:]\s*"
    r"(?:\"(?P<dq>[^\"\n]*)\"|'(?P<sq>[^'\n]*)'|(?P<bare>[^\s\"']+))"
)
PASSWORD_LIKE = re.compile(r"(?i)(pass|pwd|pw|secret|credential)")


def findings(text):
    found = [label for label, pattern in TOKEN_FORMATS if re.search(pattern, text)]
    for m in NAMED.finditer(text):
        name = m["name"]
        value = m["dq"] if m["dq"] is not None else m["sq"] if m["sq"] is not None else m["bare"]
        normalised = re.sub(r"[-_\s]", "", name.lower())
        before = text[max(0, m.start() - 12):m.start()].lower()
        if normalised.endswith("idempotencykey") or before.endswith("idempotency "):
            continue
        value = value.rstrip(",;.)")
        if value.strip() in ("REDACTED", ""):
            continue
        if normalised == "key":
            if len(value) < 16 or not (re.search(r"\d", value) and re.search(r"[A-Za-z]", value)):
                continue
        elif len(value) < (4 if PASSWORD_LIKE.search(name) else 8):
            continue
        found.append(f"a value for {name}")
    return found


def self_test():
    refuse = [
        "token=ATTA0123456789abcdef0123456789abcdef",
        "see ghp_abcdefghijklmnopqrstuvwxyz12",
        "Authorization: Bearer abcdefghijklmnopqrstuvwxyz",
        "authorization: bearer abcdefghijklmn",
        "password: hunter2hunter2hunter2",
        "password: Hunter2-Cake!9",
        'api_key: "abcdefghijklmnop1234"',
        "access_token=abcdefgh12345678",
        "client_secret=abcdefgh1234",
        "STAFF_PW=correct-horse-9",
        "GK_STAFF_PASSWORD: s3cretvalue",
        "postgres://gk:pass1234@db.example:5432/gk",
        "sk_live_abcdefghijkl and whsec_abcdefghijkl",
        "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.sig",
        "-----BEGIN OPENSSH PRIVATE KEY-----",
        "Authorization: Basic dXNlcjpwYXNzd29yZA==",
        "password: \"it's-a-secret-99\"",
        "password: Pa,ssword-123456",
        "STAFF_PW='correct horse battery staple'",
        "password: hunter2",
        "DB_PASS=abcd1234",
        "pass: letmein1",
        '{"credentials": "abcdefgh12345678"}',
        "Cookie: session=abcdef0123456789",
        "secret_key_base=abcdefgh12345678",
        "https://0123456789abcdef0123456789abcdef@o1.ingest.sentry.io/1",
        "postgres://gk:pa/ss1234@db.example/gk",
    ]
    allow = [
        "key=REDACTED and token=REDACTED",
        "Idempotency-Key=550e8400-e29b-41d4-a716-446655440000",
        "Bearer REDACTED",
        "plain report, PR #49, merge 137bc3e, colour key Pink",
        "The chef's key decision: Pink first",
        "postgres://gk:REDACTED@db.example/gk",
        "sb_publishable_2Ykzv4WThHGbG1LJlAksWg_5sbxfkfc is the public key",
        "Idempotency key: 550e8400-e29b-41d4-a716-446655440000",
        "idempotency_key=550e8400-e29b-41d4-a716-446655440000",
        "idempotencyKey: 550e8400-e29b-41d4-a716-446655440000",
        "Primary key: customer_identifier",
        "monkey: banana-bread-recipe",
        "token budget is fine; the key point: Pink first",
        "password: REDACTED",
    ]
    bad = [t for t in refuse if not findings(t)] + [f"allowed wrongly refused: {t}" for t in allow if findings(t)]
    if bad:
        sys.exit("self-test failed:\n" + "\n".join(bad))
    print("credential check self-test passed")


if __name__ == "__main__":
    if sys.argv[1:] == ["--self-test"]:
        self_test()
    elif len(sys.argv) == 2:
        reasons = findings(open(sys.argv[1], encoding="utf-8").read())
        if reasons:
            sys.exit("looks like it contains a credential: " + ", ".join(sorted(set(reasons))))
    else:
        sys.exit(__doc__)
