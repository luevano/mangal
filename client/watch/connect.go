package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/proc"
	"github.com/luevano/mangal/v5/util/tlsutil"
)

// Discovery is the file a running `mangal serve` writes so the CLI and the TUI of the same
// config (data db) find it, see DiscoveryPath.
type Discovery struct {
	PID     int       `json:"pid"`
	Address string    `json:"address"`
	Started time.Time `json:"started"`
	Version string    `json:"version"`
	// Data is the data database the server uses.
	Data string `json:"data"`
	// Key is what mangal on the same config sends (Authorization: Bearer), every
	// permission: the file is only readable by its user, as the data db.
	Key string `json:"key,omitempty"`
}

// DataDB is the data database of the config in use.
func DataDB() string {
	p := filepath.Join(path.Data(), storage.FilenameData)
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// DiscoveryPath is the discovery file of the server of the data database:
// <state dir>/mangal/serve-<hash of its path>.json, one per instance.
func DiscoveryPath(dataDB string) string {
	sum := sha256.Sum256([]byte(dataDB))
	return filepath.Join(xdg.StateHome, meta.AppName, "serve-"+hex.EncodeToString(sum[:6])+".json")
}

// WriteDiscovery writes the file atomically.
func WriteDiscovery(file string, d Discovery) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err := afs.WriteFileAtomic(file, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	return nil
}

// ReadDiscovery reads the file, false if there's none.
func ReadDiscovery(file string) (Discovery, bool, error) {
	var d Discovery
	b, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return d, false, nil
	}
	if err != nil {
		return d, false, err
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return d, false, fmt.Errorf("read %s: %w", file, err)
	}
	return d, true, nil
}

// Find is the running server of the data database, nil when none answers, authenticated
// with the discovery file's key. A discovery file of a server that doesn't run anymore is
// removed.
func Find(ctx context.Context, dataDB string) (*Remote, Discovery, error) {
	file := DiscoveryPath(dataDB)
	d, found, err := ReadDiscovery(file)
	if err != nil || !found {
		return nil, d, err
	}
	if d.Data != dataDB || !proc.Alive(d.PID) {
		_ = os.Remove(file)
		return nil, d, nil
	}
	remote := NewRemote(d.Address, d.Key)
	if remote.Client, err = LocalClient(d.Address, remote.Client.Timeout); err != nil {
		return nil, d, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := remote.Status(ctx); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
			return nil, d, fmt.Errorf("mangal serve runs on %s: %w", d.Address, err)
		}
		// still starting, or another process with a reused pid: work locally
		return nil, d, nil
	}
	return remote, d, nil
}

// LocalClient is the HTTP client of this config's own server at address: with HTTPS
// (serve.tls_cert) it trusts the certificate of the config's file, whatever names it has
// (the address is a loopback one).
func LocalClient(address string, timeout time.Duration) (*http.Client, error) {
	cert := config.Serve.TLSCert.Get()
	if cert == "" || !strings.HasPrefix(address, "https://") {
		return &http.Client{Timeout: timeout}, nil
	}
	return tlsutil.PinnedClient(cert, timeout)
}

// Connect is the Manager the CLI and the TUI use: the running `mangal serve` of this
// config's data database (found with its discovery file) when there's one, else the local
// Service (New) with the notifier (notify.Watches(), which this package can't import).
func Connect(ctx context.Context, notifier Notifier) (Manager, error) {
	remote, _, err := Find(ctx, DataDB())
	if err != nil {
		return nil, err
	}
	if remote != nil {
		return remote, nil
	}
	return New(notifier)
}

// LocalAddress is the address to reach a server listening on addr: a loopback one when
// it listens on every interface.
func LocalAddress(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return "http://" + addr.String()
	}
	host := tcp.IP
	if host.IsUnspecified() {
		host = net.IPv4(127, 0, 0, 1)
	}
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host.String(), fmt.Sprint(tcp.Port))}
	return u.String()
}
