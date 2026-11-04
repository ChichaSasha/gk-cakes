package recovery

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Sealer encrypts recovery envelopes with AES-256-GCM before they leave the
// process (app-side encryption on top of B2's SSE-B2). The object name is the
// associated data, so an envelope only opens under the name it was sealed for.
type Sealer struct {
	current  string
	order    []string // key IDs in configuration order (current first)
	keys     map[string]cipher.AEAD
	nameKeys map[string][]byte // per key: derived key for blob object names
}

var kidPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// NewSealer parses "kid:base64key[,kid:base64key...]". The first key seals;
// every listed key opens, so a rotated key can still read older envelopes.
// Errors never include key material.
func NewSealer(spec string) (*Sealer, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, errors.New("recovery: no encryption key configured")
	}
	s := &Sealer{keys: map[string]cipher.AEAD{}, nameKeys: map[string][]byte{}}
	for i, entry := range strings.Split(spec, ",") {
		kid, encoded, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || !kidPattern.MatchString(kid) {
			return nil, fmt.Errorf("recovery: encryption key %d must be kid:base64 with a lower-case kid", i+1)
		}
		if _, dup := s.keys[kid]; dup {
			return nil, fmt.Errorf("recovery: encryption key id %q is listed twice", kid)
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("recovery: encryption key %q must be 32 bytes, base64-encoded", kid)
		}
		block, err := aes.NewCipher(raw)
		if err != nil {
			return nil, fmt.Errorf("recovery: encryption key %q: %w", kid, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("recovery: encryption key %q: %w", kid, err)
		}
		s.keys[kid] = aead
		names := hmac.New(sha256.New, raw)
		names.Write([]byte("gk-recovery blob name v1"))
		s.nameKeys[kid] = names.Sum(nil)
		s.order = append(s.order, kid)
		if i == 0 {
			s.current = kid
		}
	}
	return s, nil
}

type sealed struct {
	V     int    `json:"v"`
	Alg   string `json:"alg"`
	KID   string `json:"kid"`
	Nonce string `json:"nonce"`
	CT    string `json:"ct"`
}

// Seal encrypts plaintext for the object called name.
func (s *Sealer) Seal(name string, plaintext []byte) ([]byte, error) {
	aead := s.keys[s.current]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("recovery: nonce: %w", err)
	}
	return json.Marshal(sealed{
		V:     1,
		Alg:   "A256GCM",
		KID:   s.current,
		Nonce: base64.StdEncoding.EncodeToString(nonce),
		CT:    base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, []byte(name))),
	})
}

// blobTokens returns the object-name tokens for a content hash, one per key
// (current first): "<kid>-<hex HMAC>". Keyed names stop anyone who can list
// the bucket from confirming that a known file is stored.
func (s *Sealer) blobTokens(sum string) []string {
	tokens := make([]string, 0, len(s.order))
	for _, kid := range s.order {
		mac := hmac.New(sha256.New, s.nameKeys[kid])
		mac.Write([]byte(sum))
		tokens = append(tokens, kid+"-"+hex.EncodeToString(mac.Sum(nil)))
	}
	return tokens
}

// ErrUnreadable means an envelope could not be authenticated and decrypted.
var ErrUnreadable = errors.New("recovery: envelope cannot be opened")

// Open authenticates and decrypts an envelope sealed for name.
func (s *Sealer) Open(name string, data []byte) ([]byte, error) {
	var env sealed
	if err := json.Unmarshal(data, &env); err != nil || env.V != 1 || env.Alg != "A256GCM" {
		return nil, ErrUnreadable
	}
	aead, ok := s.keys[env.KID]
	if !ok {
		return nil, fmt.Errorf("%w: unknown key id %q", ErrUnreadable, env.KID)
	}
	nonce, err1 := base64.StdEncoding.DecodeString(env.Nonce)
	ct, err2 := base64.StdEncoding.DecodeString(env.CT)
	if err1 != nil || err2 != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrUnreadable
	}
	plain, err := aead.Open(nil, nonce, ct, []byte(name))
	if err != nil {
		return nil, ErrUnreadable
	}
	return plain, nil
}
