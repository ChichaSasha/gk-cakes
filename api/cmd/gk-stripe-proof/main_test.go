package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRefusesLiveAndMissingKeys(t *testing.T) {
	for _, key := range []string{"", "sk_live_c4n4ry", "rk_live_c4n4ry", "pk_test_c4n4ry"} {
		var out bytes.Buffer
		if code := run(context.Background(), func(k string) string {
			if k == "STRIPE_SECRET_KEY" {
				return key
			}
			return ""
		}, &out, "http://127.0.0.1:1"); code != 2 {
			t.Errorf("%q: exit %d, want 2", key, code)
		}
		if strings.Contains(out.String(), "c4n4ry") {
			t.Errorf("%q: output leaks the key: %s", key, out.String())
		}
	}
}
