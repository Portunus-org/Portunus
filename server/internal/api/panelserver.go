package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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
	guard  gateGuard
	// shared is the certificate last written out for the connections.
	shared [sha256.Size]byte
	// decoy is the address of the operator's cover site, cover the site
	// the panel made itself, shown when there is none; decoyStop closes
	// the cover site's loopback listener.
	decoy     *url.URL
	cover     http.Handler
	coverFor  string
	decoyStop func()
	// The domain the server is moving to (core: MoveDomainTo): why its
	// certificate has not come yet, and when it is asked for again.
	nextErr   string
	nextRetry time.Time
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

// DomainMoveError says why the certificate of the domain the server is
// moving to has not been obtained ("" while all goes well).
func (s *Server) DomainMoveError() string {
	s.panel.mu.Lock()
	defer s.panel.mu.Unlock()
	return s.panel.nextErr
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
		key = strings.Join([]string{pa.Domain, strconv.Itoa(pa.Port), pa.Mode, hex.EncodeToString(sum[:8]), strconv.FormatBool(pa.Shared), pa.Next}, "|")
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
	if pa.Next == "" {
		s.panel.nextErr = ""
	}
	// The new domain's certificate is asked for again after a refusal,
	// once its pause is over.
	again := same && key != "" && pa.Next != "" && pa.Mode != core.PanelCertOwn && time.Now().After(s.panel.nextRetry)
	if again {
		s.panel.nextRetry = time.Now().Add(2 * time.Minute)
	}
	s.panel.mu.Unlock()
	if again {
		go s.warmUp(pa.Next)
	}
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
		s.shareCertificate(pair)
		tlsCfg = &tls.Config{
			MinVersion: tls.VersionTLS12,
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
			HostPolicy: autocert.HostWhitelist(slices.DeleteFunc([]string{pa.Domain, pa.Next}, func(h string) bool { return h == "" })...),
			Cache:      autocert.DirCache(filepath.Join(s.cfg.DataDir, "panel-acme")),
			Email:      s.core.ACMEEmail(ctx),
		}
		if s.cfg.ACMEDirectory != "" {
			m.Client = &acme.Client{DirectoryURL: s.cfg.ACMEDirectory}
		}
		tlsCfg = m.TLSConfig()
		get := tlsCfg.GetCertificate
		// After a refusal the next attempt waits. Every visitor's connection
		// would otherwise ask Let's Encrypt again, and repeated asking is
		// itself counted against the domain.
		var (
			waitMu    sync.Mutex
			waitUntil time.Time
			waitErr   error
			slowSaid  time.Time
		)
		tlsCfg.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			ours := strings.EqualFold(hello.ServerName, pa.Domain)
			// Let's Encrypt's own check of the domain is always answered.
			validating := slices.Contains(hello.SupportedProtos, acme.ALPNProto)
			if pa.Next != "" && strings.EqualFold(hello.ServerName, pa.Next) && !validating {
				return s.nextCertificate(get, hello, pa.Next)
			}
			if ours && !validating {
				waitMu.Lock()
				until, last := waitUntil, waitErr
				waitMu.Unlock()
				if time.Now().Before(until) {
					return nil, last
				}
			}
			// A request that neither succeeds nor fails is said so, here and
			// on the page: it means that this server cannot reach Let's
			// Encrypt, or Let's Encrypt cannot reach it, and nothing else
			// would ever tell.
			done := make(chan struct{})
			started := time.Now()
			if ours && !validating {
				go func() {
					select {
					case <-done:
					case <-time.After(30 * time.Second):
						s.setPanelError("Let's Encrypt: still no answer after 30 seconds. Either this server cannot reach Let's Encrypt " +
							"(acme-v02.api.letsencrypt.org, port 443), or Let's Encrypt cannot reach this server on port 443 by the domain.")
						waitMu.Lock()
						quiet := time.Since(slowSaid) < 2*time.Minute
						if !quiet {
							slowSaid = time.Now()
						}
						waitMu.Unlock()
						if !quiet {
							s.log.Warn("the certificate for the panel is taking long: no answer from Let's Encrypt in 30 seconds", "domain", pa.Domain)
						}
					}
				}()
			}
			cert, err := get(hello)
			close(done)
			if ours && !validating && err == nil && time.Since(started) > 2*time.Second {
				s.log.Info("certificate for the panel obtained", "domain", pa.Domain, "took", time.Since(started).Round(time.Second))
			}
			// Only failures for our own name say something about the
			// certificate; the rest is strangers knocking.
			if ours && !validating {
				switch {
				case err == nil:
					s.setPanelError("")
					s.shareCertificate(cert)
				case strings.Contains(err.Error(), "missing certificate"):
					// Said to a connection that waited for another one's
					// request: that one got the reason, and it is kept.
				default:
					msg, pause := acmeFailure(err)
					waitMu.Lock()
					waitUntil, waitErr = time.Now().Add(pause), err
					waitMu.Unlock()
					s.setPanelError(msg)
					s.log.Warn("no certificate for the panel", "domain", pa.Domain, "err", err, "next_try_in", pause.Round(time.Second))
				}
			}
			return cert, err
		}
	}
	// The floor, not the version used: a browser that has TLS 1.3, as all
	// current ones do, gets 1.3. Nothing older than 1.2 is spoken at all.
	tlsCfg.MinVersion = tls.VersionTLS12
	// With Let's Encrypt the certificate is asked for now and not at the
	// first visit (the connections that serve the same certificate should
	// not have to wait for somebody to open the panel): see warmUp below,
	// started once the listeners are up.
	warm := pa.Mode != core.PanelCertOwn

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
	if warm {
		go s.warmUp(pa.Domain)
		if pa.Next != "" {
			s.panel.mu.Lock()
			s.panel.nextRetry = time.Now().Add(2 * time.Minute)
			s.panel.mu.Unlock()
			go s.warmUp(pa.Next)
		}
	}
}

// nextCertificate obtains the certificate of the domain the server is
// moving to. Once it is there, the server moves: the panel, the
// subscriptions and the connections go over to the new name together.
func (s *Server) nextCertificate(get func(*tls.ClientHelloInfo) (*tls.Certificate, error), hello *tls.ClientHelloInfo, next string) (*tls.Certificate, error) {
	cert, err := get(hello)
	if err != nil {
		if !strings.Contains(err.Error(), "missing certificate") {
			msg, pause := acmeFailure(err)
			s.panel.mu.Lock()
			s.panel.nextErr, s.panel.nextRetry = msg, time.Now().Add(pause)
			s.panel.mu.Unlock()
			s.log.Warn("no certificate for the new domain yet", "domain", next, "err", err, "next_try_in", pause.Round(time.Second))
		}
		return nil, err
	}
	// Moved from outside a handshake: moving rebuilds this listener.
	go func() {
		ctx := context.Background()
		if pa, err := s.core.PanelAccess(ctx); err != nil || !strings.EqualFold(pa.Next, next) {
			return // moved already, or the move was called off
		}
		if err := s.core.MoveDomain(ctx, next); err != nil {
			s.log.Error("move to the new domain", "domain", next, "err", err)
			s.panel.mu.Lock()
			s.panel.nextErr = err.Error()
			s.panel.mu.Unlock()
			return
		}
		s.kickPanelAccess()
		s.kickSubscriptions()
	}()
	return cert, nil
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

// warmUp has the certificate asked for, by connecting to our own listener
// the way a browser would. It has to be a real connection: what is asked
// of Let's Encrypt depends on what the client says it can do, and anything
// else here (a made-up greeting was tried) orders a second certificate, of
// another kind, at the same time as the first real visitor's, the two then
// getting in each other's way. Through the listener there is one order,
// whoever comes first, and everybody else waits for it.
func (s *Server) warmUp(domain string) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		Config:    &tls.Config{MinVersion: tls.VersionTLS12, ServerName: domain, InsecureSkipVerify: true}, // nosemgrep: go.lang.security.audit.net.use-tls.use-tls, problem-based-packs.insecure-transport.go-stdlib.bypass-tls-verification.bypass-tls-verification -- a handshake with ourselves, only to have the certificate obtained; nothing is sent
	}
	if conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(core.PanelInnerPort))); err == nil {
		conn.Close()
	}
}

// shareCertificate puts the certificate the panel serves its HTTPS with
// into files, for the connections on the same domain to serve as well
// (core: shareCertificate), and has the configuration applied when that
// changes anything. It is called for every handshake, so it does nothing
// while the certificate stays the same.
func (s *Server) shareCertificate(cert *tls.Certificate) {
	certFile, keyFile := s.core.PanelCertFiles()
	if certFile == "" || cert == nil || len(cert.Certificate) == 0 {
		return
	}
	sum := sha256.Sum256(cert.Certificate[0])
	s.panel.mu.Lock()
	same := s.panel.shared == sum
	s.panel.shared = sum
	s.panel.mu.Unlock()
	if same {
		return
	}
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		s.log.Error("share the panel certificate", "err", err)
		return
	}
	var chain bytes.Buffer
	for _, der := range cert.Certificate {
		pem.Encode(&chain, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	// Each file replaced in one step: sing-box never reads half of one.
	write := func(path string, data []byte) error {
		tmp := path + ".new"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
	err = os.MkdirAll(filepath.Dir(certFile), 0o700)
	if err == nil {
		err = write(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}))
	}
	if err == nil {
		err = write(certFile, chain.Bytes())
	}
	if err != nil {
		s.log.Error("share the panel certificate", "err", err)
		return
	}
	s.log.Info("the panel's certificate is available to the connections on its domain")
	// Not from here: this runs inside a handshake, and applying the
	// configuration may come back to this listener.
	go s.core.Reconcile(context.Background())
}

// acmeFailure puts a refusal by Let's Encrypt into words for the operator
// and says how long to wait before asking again.
func acmeFailure(err error) (msg string, pause time.Duration) {
	pause = time.Minute
	var ae *acme.Error
	if !errors.As(err, &ae) && strings.Contains(err.Error(), ":rateLimited") {
		// The same refusal, come wrapped in a way that hides its type.
		ae = &acme.Error{ProblemType: "urn:ietf:params:acme:error:rateLimited", Detail: err.Error(), Header: http.Header{}}
	}
	if ae != nil {
		if strings.HasSuffix(ae.ProblemType, ":rateLimited") {
			// Their answer names the limit and when it ends.
			pause = 15 * time.Minute
			if ra := ae.Header.Get("Retry-After"); ra != "" {
				if secs, e := strconv.Atoi(ra); e == nil && secs > 0 {
					pause = min(time.Duration(secs)*time.Second, time.Hour)
				}
			}
			return "Let's Encrypt will not issue a certificate for this domain for now: too many were requested lately. " +
				"Nothing is wrong with the setup; it works again by itself when the limit ends. Their answer: " + ae.Detail, pause
		}
		return "Let's Encrypt: " + ae.Detail, pause
	}
	return "Let's Encrypt: " + err.Error(), pause
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
			// nosemgrep: go.lang.security.audit.xss.no-io-writestring-to-responsewriter.no-io-writestring-to-responsewriter -- plain text, and only the one address the panel itself made up a moment ago gets here
			io.WriteString(w, strings.TrimPrefix(r.URL.Path, "/"))
			return
		}
		who := remoteHost(r)
		if gate != "" && r.Method == http.MethodGet && hmac.Equal([]byte(r.URL.Path), []byte("/"+gate)) && !s.panel.guard.shut(who) {
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
		// A stranger asking for something that looks like a secret path may
		// be guessing it.
		if r.Method == http.MethodGet && gateShaped(r.URL.Path) {
			s.panel.guard.miss(who)
		}
		decoy.ServeHTTP(w, r)
	})
}

// The secret path is a single word, and a word can be guessed by trying
// them all. So whoever asks for more than gateGuesses word-like paths that
// are not it within gateWindow finds the gate shut for the rest of that
// time: even the right word then shows the cover site. With about seven
// thousand words that makes guessing from one address a matter of days,
// and it shows in the log long before.
//
// Browsers that are already through carry the cookie and are not affected.
// Behind a VLESS inbound sharing the port every visitor comes from the
// loopback address, and the count is then common to all of them.
const (
	gateGuesses = 10
	gateWindow  = 10 * time.Minute
)

type gateGuard struct {
	mu     sync.Mutex
	misses map[string][]time.Time
}

func gateShaped(path string) bool {
	if len(path) < 4 || len(path) > 10 || path[0] != '/' {
		return false
	}
	for _, c := range path[1:] {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// recent drops what is older than the window. Callers hold mu.
func (g *gateGuard) recent(who string, now time.Time) []time.Time {
	list := g.misses[who]
	for len(list) > 0 && now.Sub(list[0]) > gateWindow {
		list = list[1:]
	}
	if len(list) == 0 {
		delete(g.misses, who)
	} else {
		g.misses[who] = list
	}
	return list
}

func (g *gateGuard) miss(who string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if g.misses == nil {
		g.misses = map[string][]time.Time{}
	}
	// Not a place to keep a list of everybody who ever came by.
	if len(g.misses) > 20000 {
		for k := range g.misses {
			g.recent(k, now)
		}
	}
	if list := g.recent(who, now); len(list) <= gateGuesses {
		g.misses[who] = append(list, now)
	}
}

func (g *gateGuard) shut(who string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.recent(who, time.Now())) >= gateGuesses
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
	// Final: waiting will not help, something has to be changed first.
	// Anything else is the certificate still on its way.
	final := func(msg string) bool {
		return msg != "" && (!strings.HasPrefix(msg, "Let's Encrypt") || strings.HasPrefix(msg, "Let's Encrypt will not issue"))
	}
	if msg := s.PanelAccessError(); final(msg) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": msg, "final": true})
		return
	}
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		// The chain is verified below, against the domain: the connection
		// itself goes to this very machine by its loopback address.
		Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: pa.Domain, InsecureSkipVerify: true}, // nosemgrep: go.lang.security.audit.net.use-tls.use-tls, problem-based-packs.insecure-transport.go-stdlib.bypass-tls-verification.bypass-tls-verification -- a handshake with ourselves; the certificate is verified explicitly below
	}
	// Short, so that the page hears how things stand every few seconds
	// and can show it. Within that time the handshake is tried again and
	// again: right after the panel is switched on the listener may not be
	// up yet, and a certificate from Let's Encrypt takes from seconds to a
	// minute. The page asks again when this runs out; the request for the
	// certificate itself goes on meanwhile and is not started anew.
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var conn net.Conn
	for {
		conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(core.PanelInnerPort)))
		if err == nil {
			break
		}
		msg := s.PanelAccessError()
		if final(msg) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": msg, "final": true})
			return
		}
		select {
		case <-ctx.Done():
			// Nothing has gone wrong that we know of: the certificate is
			// simply not there yet. What Let's Encrypt last said, if it
			// said anything, goes along for the page to show.
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "waiting": true, "error": msg})
			return
		case <-time.After(2 * time.Second):
		}
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
