package recovery

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func testKey(fill byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

func TestSealOpenRoundTripBoundToName(t *testing.T) {
	s, err := NewSealer("k1:" + testKey(1))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"order":"secret details"}`)
	sealed, err := s.Seal("recovery/test/op/a", plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("secret details")) {
		t.Fatal("sealed envelope contains plaintext")
	}
	got, err := s.Open("recovery/test/op/a", sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("Open = %q, %v; want the plaintext", got, err)
	}
	// The ciphertext is bound to its object name: a copy moved elsewhere fails.
	if _, err := s.Open("recovery/test/op/b", sealed); err == nil {
		t.Fatal("envelope opened under a different name")
	}
	// Tampering fails authentication.
	tampered := bytes.Replace(sealed, []byte(`"ct":"`), []byte(`"ct":"A`), 1)
	if _, err := s.Open("recovery/test/op/a", tampered); err == nil {
		t.Fatal("tampered envelope opened")
	}
	// Two seals of the same plaintext differ (random nonce).
	again, _ := s.Seal("recovery/test/op/a", plain)
	if bytes.Equal(again, sealed) {
		t.Fatal("sealing is deterministic; nonces repeat")
	}
}

// TestKeyRotation: the first key seals; every listed key opens.
func TestKeyRotation(t *testing.T) {
	old, _ := NewSealer("k1:" + testKey(1))
	sealedOld, _ := old.Seal("n", []byte("x"))
	rotated, err := NewSealer("k2:" + testKey(2) + ",k1:" + testKey(1))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := rotated.Open("n", sealedOld); err != nil || string(got) != "x" {
		t.Fatalf("rotated sealer cannot open an old envelope: %q, %v", got, err)
	}
	sealedNew, _ := rotated.Seal("n", []byte("y"))
	if !strings.Contains(string(sealedNew), `"kid":"k2"`) {
		t.Fatalf("rotated sealer did not seal with the first key: %s", sealedNew)
	}
	if _, err := old.Open("n", sealedNew); err == nil {
		t.Fatal("old sealer opened an envelope sealed with an unknown key")
	}
}

func TestNewSealerRejectsBadKeysWithoutEchoingThem(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too-short-key"))
	for _, spec := range []string{
		"",
		"k1",
		"k1:",
		"k1:" + short,
		"k1:not base64!",
		"K1:" + testKey(1),
		"k1:" + testKey(1) + ",k1:" + testKey(2),
		"k1:" + testKey(1) + ",",
	} {
		_, err := NewSealer(spec)
		if err == nil {
			t.Errorf("NewSealer(%q) accepted", spec)
			continue
		}
		for _, secret := range []string{testKey(1), testKey(2), short} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("NewSealer error echoes key material: %v", err)
			}
		}
	}
}
