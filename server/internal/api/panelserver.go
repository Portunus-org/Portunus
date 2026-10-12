package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/PaulichP/Portunus/server/internal/core"
	"github.com/PaulichP/Portunus/server/internal/cover"
)

// The panel by its domain: an HTTPS listener the panel manages itself, next
// to the loopback one used through an SSH tunnel (core: PanelAccess). The
// certificate comes from Let's Encrypt, obtained on the first connection and
// renewed in time (autocert), or is the operator's own.
//
// Probing resistance. A stranger who connects, whether a person, a scanner
// or a censor's probe, must not learn that a panel lives here:
//
//   - without the name of the domain in the TLS handshake there is no
//     handshake at all, as on any server that hosts named sites only;
//   - every request from a browser that has not been through the gate is
//     answered by the cover site (decoy): a small ordinary site the panel
//     makes itself (package cover), or another site of the operator's
//     choice, fetched and passed on. That goes for
//     the login page, the API and the scripts alike. A refusal or an empty
//     answer would itself be a tell; a site is what a domain is for;
//   - the gate is a secret path (https://domain/<gate>). Opening it marks
//     the browser with a cookie and sends it on to the panel.

type panelState struct {
	mu     sync.Mutex
	key    string // what the running listener was started with
	err    string // why it is not running, or why there is no certificate
	kick   chan struct{}
	close  func()
	gate   string
	token  string
	probes map[string]time.Time
	// decoy is the address of the operator's cover site, cover the site
	// the panel made itself, shown when there is none; decoyStop closes
	// the cover site's loopback listener.
	decoy     *url.URL
	cover     http.Handler
	coverFor  string
	decoyStop func()
}

const gateCookie = "portunus_gate"

func (s *Server) kickPanelAccess() {
	select {
	case s.panel.kick <- struct{}{}:
	default:
	}
}

// PanelAccessError says why the panel cannot be reached by its domain
// although it should be ("" when all is well, as far as the panel knows).
func (s *Server) PanelAccessError() string {
	s.panel.mu.Lock()
	defer s.panel.mu.Unlock()
	return s.panel.err
}

func (s *Server) setPanelError(msg string) {
	s.panel.mu.Lock()
	s.panel.err = msg
	s.panel.mu.Unlock()
}

// RunPanelAccess keeps the HTTPS listener in line with the settings until
// ctx is done.
func (s *Server) RunPanelAccess(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	defer s.stopPanelAccess()
	s.serveDecoy()
	defer func() {
		if s.panel.decoyStop != nil {
			s.panel.decoyStop()
		}
	}()
	for {
		s.syncPanelAccess(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.panel.kick:
		}
	}
}

func (s *Server) stopPanelAccess() {
	s.panel.mu.Lock()
	defer s.panel.mu.Unlock()
	if s.panel.close != nil {
		s.panel.close()
		s.panel.close = nil
	}
	s.panel.key = ""
}

func (s *Server) syncPanelAccess(ctx context.Context) {
	pa, err := s.core.PanelAccess(ctx)
	if err != nil {
		return
	}
	sum := sha256.Sum256([]byte(pa.Certificate))
	key := ""
	if pa.Enabled {
		key = strings.Join([]string{pa.Domain, strconv.Itoa(pa.Port), pa.Mode, hex.EncodeToString(sum[:8]), strconv.FormatBool(pa.Shared)}, "|")
	}
	var decoy *url.URL
	if pa.Decoy != "" {
		decoy, _ = url.Parse(pa.Decoy)
	}
	s.panel.mu.Lock()
	// A new gate or cover site needs no new listener.
	s.panel.gate, s.panel.token, s.panel.decoy = pa.Gate, s.core.PanelGateToken(pa.Gate), decoy
	if s.panel.cover == nil || s.panel.coverFor != pa.Domain {
		s.panel.cover, s.panel.coverFor = cover.New(s.core.CoverSeed(), pa.Domain), pa.Domain
	}
	same := key == s.panel.key && (key == "" || s.panel.close != nil)
	s.panel.mu.Unlock()
	if same {
		return
	}
	s.stopPanelAccess()
	if key == "" {
		s.setPanelError("")
		return
	}

	var tlsCfg *tls.Config
	switch pa.Mode {
	case core.PanelCertOwn:
		pair, err := pa.KeyPair()
		if err != nil {
			s.setPanelError("the certificate cannot be used: " + err.Error())
			return
		}
		tlsCfg = &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				if !strings.EqualFold(hello.ServerName, pa.Domain) {
					return nil, errors.New("not served here")
				}
				return pair, nil
			},
		}
	default:
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(pa.Domain),
			Cache:      autocert.DirCache(filepath.Join(s.cfg.DataDir, "panel-acme")),
			Email:      s.core.ACMEEmail(ctx),
		}
		if s.cfg.ACMEDirectory != "" {
			m.Client = &acme.Client{DirectoryURL: s.cfg.ACMEDirectory}
		}
		tlsCfg = m.TLSConfig()
		get := tlsCfg.GetCertificate
		tlsCfg.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert, err := get(hello)
			// Only failures for our own name say something about the
			// certificate; the rest is strangers knocking.
			if strings.EqualFold(hello.ServerName, pa.Domain) {
				if err != nil {
					s.setPanelError("Let's Encrypt: " + err.Error())
				} else {
					s.setPanelError("")
				}
			}
			return cert, err
		}
	}
	tlsCfg.MinVersion = tls.VersionTLS12

	newServer := func() *http.Server {
		return &http.Server{
			Handler:           s.gated(s.full),
			TLSConfig:         tlsCfg,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    64 << 10,
			// Failed handshakes of scanners are not news.
			ErrorLog: log.New(io.Discard, "", 0),
		}
	}
	// Always on loopback: this is where a VLESS connection that shares the
	// panel's port sends everybody who is not its client.
	inner, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(core.PanelInnerPort)))
	if err != nil {
		s.setPanelError(err.Error())
		s.log.Error("panel https listener", "port", core.PanelInnerPort, "err", err)
		return
	}
	servers := []*http.Server{newServer()}
	go servers[0].ServeTLS(inner, "", "")
	// The public port is the panel's own unless a VLESS connection holds
	// it; then that connection is the way in.
	if !pa.Shared {
		ln, err := net.Listen("tcp", ":"+strconv.Itoa(pa.Port))
		if err != nil {
			// Not remembered as running: the next round tries again (the
			// port may still be on its way from sing-box back to us).
			servers[0].Close()
			s.setPanelError(err.Error())
			s.log.Error("panel https listener", "port", pa.Port, "err", err)
			return
		}
		servers = append(servers, newServer())
		go servers[1].ServeTLS(ln, "", "")
	}
	s.panel.mu.Lock()
	s.panel.key, s.panel.err = key, ""
	s.panel.close = func() {
		for _, srv := range servers {
			srv.Close()
		}
	}
	s.panel.mu.Unlock()
	s.log.Info("panel available by its domain", "address", pa.Origin(), "certificate", pa.Mode, "shared_with_vless", pa.Shared)
}

// serveDecoy starts the cover site on loopback, in plain HTTP: what a
// Trojan inbound hands a connection to that turned out not to be Trojan.
func (s *Server) serveDecoy() {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(core.DecoyPort)))
	if err != nil {
		s.log.Error("cover site listener", "port", core.DecoyPort, "err", err)
		return
	}
	srv := &http.Server{
		Handler:           s.decoyHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	// What arrives is what was inside the TLS a Trojan inbound took off,
	// and a browser will have agreed on HTTP/2 there: speak it unencrypted.
	srv.Protocols = new(http.Protocols)
	srv.Protocols.SetHTTP1(true)
	srv.Protocols.SetUnencryptedHTTP2(true)
	go srv.Serve(ln)
	s.panel.decoyStop = func() { srv.Close() }
}

// decoyHandler answers with the cover site. Unless the operator named
// another site, it is the one the panel made. Another site is passed the
// request as any visitor's would be, and its answer passed back: nothing of
// ours goes along (no cookies, no address of the visitor), and what would
// bind the visitor's browser to that site's rules for this domain is
// dropped.
func (s *Server) decoyHandler() http.Handler {
	target := func() (*url.URL, http.Handler) {
		s.panel.mu.Lock()
		defer s.panel.mu.Unlock()
		if s.panel.decoy != nil && s.panel.decoy.Host != "" {
			return s.panel.decoy, nil
		}
		return nil, s.panel.cover
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			target, _ := target()
			if target == nil { // changed since the request was taken
				target = &url.URL{Scheme: "http", Host: "invalid.invalid"}
			}
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Referer")
			pr.Out.Header.Del("Origin")
		},
		ModifyResponse: func(res *http.Response) error {
			for _, h := range []string{"Strict-Transport-Security", "Alt-Svc", "Set-Cookie", "Public-Key-Pins", "Report-To", "NEL"} {
				res.Header.Del(h)
			}
			// A redirect to the site's own address stays on this domain.
			if loc := res.Header.Get("Location"); loc != "" {
				if u, err := url.Parse(loc); err == nil && u.Host != "" && strings.EqualFold(u.Host, res.Request.URL.Host) {
					u.Scheme, u.Host = "", ""
					res.Header.Set("Location", u.String())
				}
			}
			return nil
		},
		Transport: &http.Transport{
			Proxy:                 nil,
			ResponseHeaderTimeout: 15 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       60 * time.Second,
		},
		ErrorLog: log.New(io.Discard, "", 0),
		// The cover site is out of reach: an ordinary "not found" is the
		// least remarkable thing left to say.
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, _ error) { http.NotFound(w, r) },
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch other, own := target(); {
		case other != nil:
			proxy.ServeHTTP(w, r)
		case own != nil:
			own.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// gated lets through only browsers that have opened the gate; see the note
// at the top of the file.
func (s *Server) gated(next http.Handler) http.Handler {
	decoy := s.decoyHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.panel.mu.Lock()
		gate, token := s.panel.gate, s.panel.token
		probe, isProbe := s.panel.probes[strings.TrimPrefix(r.URL.Path, "/")]
		s.panel.mu.Unlock()

		// The setup's own check that a browser reaches this server by the
		// domain: a one-off address, made up a moment ago.
		if isProbe && time.Now().Before(probe) && r.Method == http.MethodGet {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			io.WriteString(w, strings.TrimPrefix(r.URL.Path, "/"))
			return
		}
		if gate != "" && r.Method == http.MethodGet && hmac.Equal([]byte(r.URL.Path), []byte("/"+gate)) {
			http.SetCookie(w, &http.Cookie{
				Name: gateCookie, Value: token, Path: "/", MaxAge: 180 * 24 * 3600,
				HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
			})
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if c, err := r.Cookie(gateCookie); err == nil && gate != "" && hmac.Equal([]byte(c.Value), []byte(token)) {
			next.ServeHTTP(w, r)
			return
		}
		decoy.ServeHTTP(w, r)
	})
}

// newProbe makes up a one-off address on the HTTPS listener.
func (s *Server) newProbe() string {
	b := make([]byte, 16)
	rand.Read(b)
	id := hex.EncodeToString(b)
	s.panel.mu.Lock()
	defer s.panel.mu.Unlock()
	now := time.Now()
	for k, exp := range s.panel.probes {
		if now.After(exp) {
			delete(s.panel.probes, k)
		}
	}
	s.panel.probes[id] = now.Add(10 * time.Minute)
	return id
}

// ------------------------------------------------------------------- API

type panelAccessDTO struct {
	Enabled bool   `json:"enabled"`
	Domain  string `json:"domain"`
	Port    int    `json:"port"`
	Mode    string `json:"mode"`
	// HasCertificate: an own certificate is stored (it is never sent back).
	HasCertificate bool `json:"hasCertificate"`
	// URL is the secret entry address.
	URL string `json:"url"`
	// Decoy is the site shown to everybody who has not opened that address.
	Decoy string `json:"decoy"`
	// Shared: a VLESS connection is on the same port and passes browsers on.
	Shared bool   `json:"shared"`
	Error  string `json:"error,omitempty"`
}

func (s *Server) panelAccessDTO(ctx context.Context) (panelAccessDTO, error) {
	pa, err := s.core.PanelAccess(ctx)
	if err != nil {
		return panelAccessDTO{}, err
	}
	port := pa.Port
	if port == 0 {
		port = core.DefaultPanelPort
	}
	out := panelAccessDTO{Enabled: pa.Enabled, Domain: pa.Domain, Port: port, Mode: pa.Mode, HasCertificate: pa.Certificate != "", URL: pa.URL(), Decoy: pa.Decoy, Shared: pa.Shared}
	if pa.Enabled {
		out.Error = s.PanelAccessError()
	}
	return out, nil
}

func (s *Server) handleGetPanelAccess(w http.ResponseWriter, r *http.Request) {
	out, err := s.panelAccessDTO(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutPanelAccess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled     bool    `json:"enabled"`
		Port        int     `json:"port"`
		Mode        string  `json:"mode"`
		Certificate *string `json:"certificate"`
		Key         *string `json:"key"`
		Decoy       *string `json:"decoy"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.core.SetPanelAccess(r.Context(), core.PanelAccessInput{Enabled: req.Enabled, Port: req.Port, Mode: req.Mode, Certificate: req.Certificate, Key: req.Key, Decoy: req.Decoy}); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "settings.panel_access", "", map[string]any{"enabled": req.Enabled, "port": req.Port, "mode": req.Mode})
	s.syncPanelAccess(r.Context())
	s.handleGetPanelAccess(w, r)
}

func (s *Server) handleRotatePanelGate(w http.ResponseWriter, r *http.Request) {
	if err := s.core.RotatePanelGate(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.auditAction(r, "settings.panel_gate_rotated", "", nil)
	s.syncPanelAccess(r.Context())
	s.handleGetPanelAccess(w, r)
}

// handleCheckPanelAccess connects to the listener the way a browser would
// and reports the certificate it is given. With Let's Encrypt this is also
// what makes the panel ask for the certificate, so it can take a while.
func (s *Server) handleCheckPanelAccess(w http.ResponseWriter, r *http.Request) {
	pa, err := s.core.PanelAccess(r.Context())
	if err != nil || !pa.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "off"})
		return
	}
	if msg := s.PanelAccessError(); msg != "" && !strings.HasPrefix(msg, "Let's Encrypt") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": msg})
		return
	}
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		// The chain is verified below, against the domain: the connection
		// itself goes to this very machine by its loopback address.
		Config: &tls.Config{ServerName: pa.Domain, InsecureSkipVerify: true}, // nosemgrep: go.lang.security.audit.net.use-tls.use-tls, problem-based-packs.insecure-transport.go-stdlib.bypass-tls-verification.bypass-tls-verification -- self-check over loopback; the certificate is verified explicitly right after
	}
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(core.PanelInnerPort)))
	if err != nil {
		msg := s.PanelAccessError()
		if msg == "" {
			msg = err.Error()
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": msg})
		return
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	leaf := certs[0]
	pool := x509.NewCertPool()
	for _, c := range certs[1:] {
		pool.AddCert(c)
	}
	_, verr := leaf.Verify(x509.VerifyOptions{DNSName: pa.Domain, Intermediates: pool})
	issuer := leaf.Issuer.CommonName
	if len(leaf.Issuer.Organization) > 0 {
		issuer = leaf.Issuer.Organization[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "issuer": issuer, "notAfter": leaf.NotAfter.UTC(),
		// Whether an ordinary browser will accept it without a warning.
		"trusted": verr == nil,
	})
}

// handlePanelProbe hands the browser a one-off address on the HTTPS
// listener to fetch: if the answer comes back, the domain leads this
// browser to this server, which a fresh DNS record does not yet do for
// everybody.
func (s *Server) handlePanelProbe(w http.ResponseWriter, r *http.Request) {
	pa, err := s.core.PanelAccess(r.Context())
	if err != nil || !pa.Enabled {
		writeError(w, http.StatusConflict, "panel_access_off")
		return
	}
	id := s.newProbe()
	writeJSON(w, http.StatusOK, map[string]string{"url": pa.Origin() + "/" + id, "expect": id})
}
