package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCert writes a self-signed certificate for name and its key, their paths.
func writeCert(t *testing.T, dir, name string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(dir, name+".crt"), filepath.Join(dir, name+".key")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	return certFile, keyFile
}

// TestTLS: HTTPS with the files; mangal's own client trusts that certificate on a loopback
// address it doesn't name, not another one.
func TestTLS(t *testing.T) {
	dir := t.TempDir()
	cert, key := writeCert(t, dir, "mangal.example.com")
	other, _ := writeCert(t, dir, "other.example.com")
	if _, err := TLSListener(nil, cert, ""); err == nil {
		t.Error("a certificate without its key")
	}
	if _, err := TLSListener(nil, cert, filepath.Join(dir, "nope.key")); err == nil {
		t.Error("a missing key")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tl, err := TLSListener(l, cert, key)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })}
	go srv.Serve(tl)
	t.Cleanup(func() { srv.Close() })
	url := "https://" + l.Addr().String() + "/"

	c, err := PinnedClient(cert, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok" || resp.TLS == nil {
		t.Errorf("got %q %v", b, resp.TLS)
	}
	if _, err := http.Get(url); err == nil {
		t.Error("an unknown certificate verified")
	}
	c, _ = PinnedClient(other, 5*time.Second)
	if _, err := c.Get(url); err == nil {
		t.Error("another certificate trusted")
	}
	if _, err := PinnedClient(filepath.Join(dir, "nope.crt"), 0); err == nil {
		t.Error("a missing certificate file")
	}
}
