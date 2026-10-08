// Package agent is the control channel between the panel and its nodes.
//
// A node runs `portunus agent`: an HTTPS server that takes a sing-box
// configuration from the panel, runs it, and reports status and traffic.
// Both sides authenticate with certificates issued by the installation's
// CA: the node proves its address, the panel proves it is the panel. The
// node keeps its last configuration on disk, so it goes on serving when
// the panel is unreachable or the node restarts.
package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PaulichP/Portunus/server/internal/proxy"
	"github.com/PaulichP/Portunus/server/internal/singbox"
	"github.com/PaulichP/Portunus/server/internal/version"
)

// DefaultPort is where the agent listens unless the bundle says otherwise.
const DefaultPort = 2056

// StatsAPI is the loopback address of sing-box's statistics service on a
// node. The panel puts it into the configs it sends.
const StatsAPI = "127.0.0.1:2054"

// Bundle is everything a node needs to join: handed to it once, as the
// PORTUNUS_JOIN environment variable.
type Bundle struct {
	Port int `json:"port"`
	// CA is the installation's CA certificate: the agent accepts only the
	// panel certificate issued by it.
	CA string `json:"ca"`
	// Cert and Key are the node's own server certificate, for its address.
	Cert string `json:"cert"`
	Key  string `json:"key"`
}

// Encode packs the bundle into one shell-safe token.
func (b Bundle) Encode() string {
	raw, _ := json.Marshal(b)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func DecodeBundle(token string) (Bundle, error) {
	var b Bundle
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return b, errors.New("PORTUNUS_JOIN is not a valid join token")
	}
	if err := json.Unmarshal(raw, &b); err != nil || b.CA == "" || b.Cert == "" || b.Key == "" {
		return b, errors.New("PORTUNUS_JOIN is not a valid join token")
	}
	if b.Port == 0 {
		b.Port = DefaultPort
	}
	return b, nil
}

// Status is what a node reports about itself.
type Status struct {
	Version string         `json:"version"`
	SingBox singbox.Status `json:"singBox"`
	// ConfigHash identifies the configuration the node runs ("" for none),
	// so the panel can tell whether it needs to send one.
	ConfigHash string `json:"configHash"`
	// PanelCopy describes the copy of the panel this node keeps (nil: none).
	PanelCopy *PanelCopy `json:"panelCopy,omitempty"`
}

// PanelCopy is a sealed backup of the panel kept on a node, so that the
// panel can be brought up there if the main server is lost. The node cannot
// read it: it is encrypted with the installation's recovery key.
type PanelCopy struct {
	// Node is this node's ID in the panel's database: what the restored
	// panel has to know to take this server's place.
	Node int64     `json:"node"`
	At   time.Time `json:"at"`
	// SHA256 of the stored data (hex).
	SHA256 string `json:"sha256"`
}

// PanelCopyRequest stores a copy of the panel; null data removes it.
type PanelCopyRequest struct {
	Node int64  `json:"node"`
	Data []byte `json:"data"`
}

// File names of the panel's copy inside a node's data directory; `portunus
// restore` reads them.
const (
	PanelCopyFile = "panel-backup"
	PanelCopyInfo = "panel-backup.json"
)

// ConfigRequest replaces the node's configuration. A null config stops
// sing-box.
type ConfigRequest struct {
	Config json.RawMessage `json:"config"`
}

// Traffic is per-user traffic since the previous call.
type Traffic struct {
	User     string `json:"user"`
	Uplink   int64  `json:"uplink"`
	Downlink int64  `json:"downlink"`
}

// HashConfig is the identifier of a configuration. It is taken over a
// canonical re-encoding, not the bytes: the configuration travels inside a
// JSON request, which reformats it, and both ends must still agree.
func HashConfig(cfg []byte) string {
	if len(cfg) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(cfg, &v); err == nil {
		if canonical, err := json.Marshal(v); err == nil {
			cfg = canonical
		}
	}
	sum := sha256.Sum256(cfg)
	return hex.EncodeToString(sum[:])
}

// ------------------------------------------------------------------ node

// Server is the node side.
type Server struct {
	mgr   *singbox.Manager
	stats *singbox.StatsClient
	dir   string
	log   *slog.Logger

	mu      sync.Mutex
	pending map[string]*Traffic // counters saved across sing-box reloads
}

func NewServer(mgr *singbox.Manager, dir string, log *slog.Logger) *Server {
	s := &Server{mgr: mgr, stats: singbox.NewStatsClient(StatsAPI), dir: dir, log: log, pending: map[string]*Traffic{}}
	// sing-box counters reset on reload; keep them until the panel asks.
	mgr.BeforeReload = func(ctx context.Context) { s.collect(ctx) }
	// What is saved for the next start must be what actually runs.
	mgr.OnRollback = func(restored []byte) {
		if err := os.WriteFile(s.savedConfigPath(), restored, 0o600); err != nil {
			log.Error("save restored configuration", "err", err)
		}
	}
	return s
}

func (s *Server) savedConfigPath() string { return filepath.Join(s.dir, "node-config.json") }

// Resume starts sing-box with the configuration saved by the last run.
func (s *Server) Resume(ctx context.Context) {
	cfg, err := os.ReadFile(s.savedConfigPath())
	if err != nil || len(cfg) == 0 {
		return
	}
	if err := s.mgr.Apply(ctx, cfg); err != nil {
		s.log.Error("saved configuration rejected", "err", err)
		return
	}
	s.log.Info("resumed with the saved configuration")
}

func (s *Server) collect(ctx context.Context) {
	if s.mgr.Status().PID == 0 {
		return
	}
	users, err := s.stats.QueryUsers(ctx)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range users {
		t := s.pending[u.User]
		if t == nil {
			t = &Traffic{User: u.User}
			s.pending[u.User] = t
		}
		t.Uplink += u.Uplink
		t.Downlink += u.Downlink
	}
}

// status reports what the node really runs: after a configuration was
// rolled back the hash is that of the restored one, so the panel sees the
// difference and learns the reason when it sends its configuration again.
func (s *Server) status() Status {
	st := Status{Version: version.Version, SingBox: s.mgr.Status(), ConfigHash: HashConfig(s.mgr.Config())}
	if raw, err := os.ReadFile(filepath.Join(s.dir, PanelCopyInfo)); err == nil {
		var pc PanelCopy
		if json.Unmarshal(raw, &pc) == nil {
			st.PanelCopy = &pc
		}
	}
	return st
}

// storePanelCopy replaces the copy of the panel, the data first and its
// description last, each atomically.
func (s *Server) storePanelCopy(req PanelCopyRequest) error {
	data, info := filepath.Join(s.dir, PanelCopyFile), filepath.Join(s.dir, PanelCopyInfo)
	if req.Data == nil {
		os.Remove(info)
		return os.Remove(data)
	}
	sum := sha256.Sum256(req.Data)
	meta, _ := json.Marshal(PanelCopy{Node: req.Node, At: time.Now().UTC(), SHA256: hex.EncodeToString(sum[:])})
	for path, content := range map[string][]byte{data: req.Data, info: meta} {
		if err := os.WriteFile(path+".tmp", content, 0o600); err != nil {
			return err
		}
	}
	if err := os.Rename(data+".tmp", data); err != nil {
		return err
	}
	return os.Rename(info+".tmp", info)
}

// Handler serves the agent API. Authentication is the TLS layer's job
// (see TLSConfig): every request that gets here comes from the panel.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.status())
	})
	mux.HandleFunc("PUT /v1/config", func(w http.ResponseWriter, r *http.Request) {
		var req ConfigRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		var cfg []byte
		if len(req.Config) > 0 && string(req.Config) != "null" {
			cfg = req.Config
		}
		if err := s.mgr.Apply(r.Context(), cfg); err != nil {
			s.log.Error("configuration from the panel rejected", "err", err)
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		if err := os.WriteFile(s.savedConfigPath(), cfg, 0o600); err != nil {
			s.log.Error("save configuration", "err", err)
		}
		s.log.Info("configuration applied", "running", cfg != nil)
		writeJSON(w, http.StatusOK, s.status())
	})
	mux.HandleFunc("PUT /v1/panel", func(w http.ResponseWriter, r *http.Request) {
		var req PanelCopyRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<20)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if err := s.storePanelCopy(req); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Error("store the panel's copy", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s.status())
	})
	mux.HandleFunc("POST /v1/traffic", func(w http.ResponseWriter, r *http.Request) {
		s.collect(r.Context())
		s.mu.Lock()
		out := make([]Traffic, 0, len(s.pending))
		for _, t := range s.pending {
			out = append(out, *t)
		}
		s.pending = map[string]*Traffic{}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// TLSConfig is the node's listener configuration: its own certificate, and
// a client certificate required from the panel.
func (b Bundle) TLSConfig() (*tls.Config, error) {
	cert, err := tls.X509KeyPair([]byte(b.Cert), []byte(b.Key))
	if err != nil {
		return nil, fmt.Errorf("node certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(b.CA)) {
		return nil, errors.New("CA certificate is not valid PEM")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		// The CA also signs server certificates; only the panel's client
		// certificate may command a node.
		VerifyPeerCertificate: func(_ [][]byte, chains [][]*x509.Certificate) error {
			for _, chain := range chains {
				if len(chain) > 0 && chain[0].Subject.CommonName == proxy.PanelCommonName {
					return nil
				}
			}
			return errors.New("client is not the panel")
		},
	}, nil
}

// ----------------------------------------------------------------- panel

// Client is the panel side of the channel to one node.
type Client struct {
	base string
	http *http.Client
}

// NewClient talks to the agent at address:port. ca verifies the node;
// certPEM and keyPEM are the panel's client certificate.
func NewClient(address string, port int, ca *proxy.CA, certPEM, keyPEM string) (*Client, error) {
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	return &Client{
		base: "https://" + net.JoinHostPort(address, fmt.Sprint(port)),
		http: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{cert}},
				TLSHandshakeTimeout: 8 * time.Second,
				IdleConnTimeout:     90 * time.Second,
				MaxIdleConns:        2,
				// The panel reaches its nodes directly, never through a proxy
				// from the environment.
				Proxy: nil,
			},
		},
	}, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return &RemoteError{Message: e.Error}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// RemoteError is a refusal from a node that was reached (for example, its
// sing-box rejected the configuration), as opposed to a network failure.
type RemoteError struct{ Message string }

func (e *RemoteError) Error() string { return e.Message }

func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	return st, c.do(ctx, http.MethodGet, "/v1/status", nil, &st)
}

func (c *Client) SetConfig(ctx context.Context, cfg []byte) (Status, error) {
	var st Status
	return st, c.do(ctx, http.MethodPut, "/v1/config", ConfigRequest{Config: cfg}, &st)
}

// Traffic returns and resets the node's per-user counters.
func (c *Client) Traffic(ctx context.Context) ([]Traffic, error) {
	var out []Traffic
	return out, c.do(ctx, http.MethodPost, "/v1/traffic", nil, &out)
}

// SetPanelCopy leaves a sealed copy of the panel on the node (nil removes
// it); node is the node's ID in the panel.
func (c *Client) SetPanelCopy(ctx context.Context, node int64, sealed []byte) (Status, error) {
	var st Status
	return st, c.do(ctx, http.MethodPut, "/v1/panel", PanelCopyRequest{Node: node, Data: sealed}, &st)
}

func (c *Client) Close() { c.http.CloseIdleConnections() }
