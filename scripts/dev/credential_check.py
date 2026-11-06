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
    ("Trello token", r"\bATTA[0-9a-fA-F]{20,}"),
    ("GitHub token", r"\bgh[pousr]_[A-Za-z0-9]{20,}|\bgithub_pat_\w{20,}"),
    ("Stripe key", r"\b[sr]k_(?:live|test)_\w{10,}|\bwhsec_\w{10,}"),
    ("Supabase secret key", r"\bsb_secret_\w{10,}"),
    ("Resend key", r"\bre_[A-Za-z0-9]{20,}"),
    ("AWS key", r"\bAKIA[0-9A-Z]{16}\b"),
    ("Slack token", r"\bxox[abprs]-[\w-]{10,}"),
    ("JWT", r"\beyJ[\w-]{10,}\.eyJ[\w-]{10,}"),
    ("bearer token", r"(?i)\bbearer\s+(?!REDACTED\b)[\w.~+/=-]{12,}"),
    ("password in a URL", r"(?i)\b[a-z][a-z0-9+.-]*://[^/\s:@]+:(?!REDACTED@)[^/\s@]+@"),
]
# NAME = value / NAME: value where NAME ends in key, token, secret, password,
# passwd, pwd or _pw (so access_token, client_secret, STAFF_PW all count).
NAMED = re.compile(
    r"(?i)(?<![\w-])(?P<name>[\w-]*?(?:key|token|secret|password|passwd|pwd|_pw))[\"']?\s*[=:]\s*[\"']?"
    r"(?P<value>[^\s\"',;&)]{8,})"
)


def findings(text):
    found = [label for label, pattern in TOKEN_FORMATS if re.search(pattern, text)]
    for m in NAMED.finditer(text):
        name, value = m["name"].lower(), m["value"]
        if name.endswith("idempotency-key") or value == "REDACTED":
            continue
        found.append(f"a value for {m['name']}")
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
    ]
    allow = [
        "key=REDACTED and token=REDACTED",
        "Idempotency-Key=550e8400-e29b-41d4-a716-446655440000",
        "Bearer REDACTED",
        "plain report, PR #49, merge 137bc3e, colour key Pink",
        "The chef's key decision: Pink first",
        "postgres://gk:REDACTED@db.example/gk",
        "sb_publishable_2Ykzv4WThHGbG1LJlAksWg_5sbxfkfc is the public key",
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
