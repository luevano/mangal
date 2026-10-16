// Package tlsutil is mangal serve's HTTPS: its listener and the client of its own server.
package tlsutil

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// TLSListener serves HTTPS on the listener with the certificate and key files (PEM; the
// certificate file may have the chain). The files are read again when they change (a
// renewal), at most once a minute, without a restart.
func TLSListener(l net.Listener, certFile, keyFile string) (net.Listener, error) {
	if (certFile == "") != (keyFile == "") {
		return nil, errors.New("serve.tls_cert and serve.tls_key go together")
	}
	r := &certReloader{cert: certFile, key: keyFile}
	if err := r.load(); err != nil {
		return nil, err
	}
	return tls.NewListener(l, &tls.Config{
		MinVersion:     tls.VersionTLS12,
		NextProtos:     []string{"h2", "http/1.1"},
		GetCertificate: r.get,
	}), nil
}

type certReloader struct {
	cert, key string

	mu      sync.Mutex
	current *tls.Certificate
	modTime time.Time
	checked time.Time
}

func (r *certReloader) load() error {
	cert, err := tls.LoadX509KeyPair(r.cert, r.key)
	if err != nil {
		return fmt.Errorf("serve.tls_cert, serve.tls_key: %w", err)
	}
	info, err := os.Stat(r.cert)
	if err != nil {
		return err
	}
	r.current, r.modTime, r.checked = &cert, info.ModTime(), time.Now()
	return nil
}

func (r *certReloader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.checked) > time.Minute {
		r.checked = time.Now()
		if info, err := os.Stat(r.cert); err == nil && !info.ModTime().Equal(r.modTime) {
			// a half-written renewal keeps the old one until the next try
			_ = r.load()
		}
	}
	return r.current, nil
}

// PinnedClient is an HTTP client for mangal's own server on this machine, served with the
// certificate file: its address is a loopback one the certificate doesn't name, so the
// server is trusted when it presents a certificate of the file.
func PinnedClient(certFile string, timeout time.Duration) (*http.Client, error) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("serve.tls_cert: %w", err)
	}
	var pinned [][]byte
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			pinned = append(pinned, block.Bytes)
		}
	}
	if len(pinned) == 0 {
		return nil, fmt.Errorf("serve.tls_cert: no certificate in %s", certFile)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		// verified below, against the file instead of a name
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) != 0 {
				for _, p := range pinned {
					if bytes.Equal(raw[0], p) {
						return nil
					}
				}
			}
			return errors.New("the server's certificate isn't serve.tls_cert's")
		},
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}
