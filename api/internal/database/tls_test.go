package database

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChichaSasha/gk-cakes/api/internal/config"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T, name string) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func (ca testCA) serverCert(t *testing.T, ips []net.IP, dns []string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "gk test server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
		DNSNames:     dns,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeTLSPostgres accepts one connection, answers PostgreSQL's SSLRequest and
// performs a TLS handshake. It reports whether the handshake succeeded.
func fakeTLSPostgres(t *testing.T, cert tls.Certificate) (addr string, handshake <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	result := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			result <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		var req [8]byte
		if _, err := io.ReadFull(conn, req[:]); err != nil {
			result <- err
			return
		}
		if binary.BigEndian.Uint32(req[4:]) != 80877103 {
			result <- errors.New("client did not request TLS")
			return
		}
		if _, err := conn.Write([]byte{'S'}); err != nil {
			result <- err
			return
		}
		srv := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		result <- srv.Handshake()
	}()
	return ln.Addr().String(), result
}

func connectVerifyFull(t *testing.T, addr string, rootPEM []byte) error {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(root, rootPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadWorker(func(k string) string {
		return map[string]string{
			"APP_ENV":      "local",
			"DATABASE_URL": "postgres://gk:pw@" + addr + "/gk?sslmode=verify-full&sslrootcert=" + root + "&connect_timeout=5",
		}[k]
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	conn, err := Connect(context.Background(), cfg.DatabaseURL, "tls-test")
	if err == nil {
		conn.Close(context.Background())
	}
	return err
}

// TestVerifyFullRejectsUntrustedOrMismatchedCertificates proves the client
// configuration accepted by config verifies both the CA and the hostname.
func TestVerifyFullRejectsUntrustedOrMismatchedCertificates(t *testing.T) {
	trusted := newTestCA(t, "trusted CA")
	other := newTestCA(t, "other CA")
	loopback := []net.IP{net.ParseIP("127.0.0.1")}

	t.Run("untrusted CA", func(t *testing.T) {
		addr, handshake := fakeTLSPostgres(t, other.serverCert(t, loopback, nil))
		err := connectVerifyFull(t, addr, trusted.pem)
		var unknown x509.UnknownAuthorityError
		if !errors.As(err, &unknown) {
			t.Fatalf("connect error = %v, want x509.UnknownAuthorityError", err)
		}
		if <-handshake == nil {
			t.Fatal("server completed a handshake the client should have rejected")
		}
	})

	t.Run("hostname mismatch", func(t *testing.T) {
		addr, handshake := fakeTLSPostgres(t, trusted.serverCert(t, nil, []string{"db.gk.test"}))
		err := connectVerifyFull(t, addr, trusted.pem)
		var mismatch x509.HostnameError
		if !errors.As(err, &mismatch) {
			t.Fatalf("connect error = %v, want x509.HostnameError", err)
		}
		if <-handshake == nil {
			t.Fatal("server completed a handshake the client should have rejected")
		}
	})

	t.Run("trusted CA and matching host", func(t *testing.T) {
		addr, handshake := fakeTLSPostgres(t, trusted.serverCert(t, loopback, nil))
		err := connectVerifyFull(t, addr, trusted.pem)
		if hsErr := <-handshake; hsErr != nil {
			t.Fatalf("TLS handshake failed with a valid certificate: %v (client: %v)", hsErr, err)
		}
		var unknown x509.UnknownAuthorityError
		var mismatch x509.HostnameError
		if errors.As(err, &unknown) || errors.As(err, &mismatch) {
			t.Fatalf("valid certificate rejected: %v", err)
		}
	})
}
