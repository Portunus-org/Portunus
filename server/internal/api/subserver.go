package api

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// Subscriptions are served by a listener the panel manages itself: its port
// comes from the settings (or PORTUNUS_SUB_LISTEN), and it speaks HTTP and
// HTTPS on the same port. HTTPS uses the domain's ACME certificate from
// sing-box's storage once it exists, so URLs handed out before the
// certificate was issued keep working afterwards.

type subState struct {
	mu    sync.Mutex
	addr  string
	err   string
	kick  chan struct{}
	close func()
}

// SubscriptionStatus reports the listener address and its last bind error.
func (s *Server) SubscriptionStatus() (addr, errText string) {
	s.sub.mu.Lock()
	defer s.sub.mu.Unlock()
	return s.sub.addr, s.sub.err
}

// kickSubscriptions makes the listener re-read its settings now.
func (s *Server) kickSubscriptions() {
	select {
	case s.sub.kick <- struct{}{}:
	default:
	}
}

// RunSubscriptions keeps the subscription listener in line with the
// settings until ctx is done.
func (s *Server) RunSubscriptions(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	defer s.stopSubscriptions()
	for {
		s.syncSubscriptions(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.sub.kick:
		}
	}
}

func (s *Server) stopSubscriptions() {
	s.sub.mu.Lock()
	defer s.sub.mu.Unlock()
	if s.sub.close != nil {
		s.sub.close()
		s.sub.close = nil
	}
	s.sub.addr = ""
}

func (s *Server) syncSubscriptions(ctx context.Context) {
	want := s.core.SubscriptionListenAddr(ctx)
	s.sub.mu.Lock()
	same := want == s.sub.addr && (want == "" || s.sub.close != nil)
	s.sub.mu.Unlock()
	if same {
		return
	}
	s.stopSubscriptions()
	if want == "" {
		s.sub.mu.Lock()
		s.sub.err = ""
		s.sub.mu.Unlock()
		return
	}

	ln, err := net.Listen("tcp", want)
	s.sub.mu.Lock()
	defer s.sub.mu.Unlock()
	s.sub.addr = want
	if err != nil {
		s.sub.err = err.Error()
		s.log.Error("subscription listener", "addr", want, "err", err)
		return
	}
	s.sub.err = ""

	var (
		certMu   sync.Mutex
		cert     *tls.Certificate
		loadedAt time.Time
	)
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			certMu.Lock()
			defer certMu.Unlock()
			if cert == nil || time.Since(loadedAt) > time.Minute {
				cert, loadedAt = s.core.SubscriptionCertificate(context.Background()), time.Now()
			}
			if cert == nil {
				return nil, errors.New("no certificate for this domain yet")
			}
			return cert, nil
		},
	}
	srv := newSubHTTPServer(s.SubscriptionHandler())
	go srv.Serve(&sniffListener{Listener: ln, tls: tlsCfg})
	s.sub.close = func() { srv.Close() }
	s.log.Info("serving subscriptions", "addr", ln.Addr().String())
}

func newSubHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

// sniffListener hands out connections that decide on their first read
// whether the peer speaks TLS (first byte 0x16, a handshake record).
type sniffListener struct {
	net.Listener
	tls *tls.Config
}

func (l *sniffListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &sniffConn{Conn: c, cfg: l.tls}, nil
}

type sniffConn struct {
	net.Conn
	cfg  *tls.Config
	once sync.Once
	rw   net.Conn
	err  error
}

func (c *sniffConn) detect() {
	br := bufio.NewReader(c.Conn)
	c.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	b, err := br.Peek(1)
	c.Conn.SetReadDeadline(time.Time{})
	if err != nil {
		c.err = err
		return
	}
	buffered := &bufferedConn{Conn: c.Conn, r: br}
	if b[0] == 0x16 {
		c.rw = tls.Server(buffered, c.cfg)
		return
	}
	c.rw = buffered
}

func (c *sniffConn) Read(p []byte) (int, error) {
	c.once.Do(c.detect)
	if c.err != nil {
		return 0, c.err
	}
	return c.rw.Read(p)
}

func (c *sniffConn) Write(p []byte) (int, error) {
	c.once.Do(c.detect)
	if c.err != nil {
		return 0, c.err
	}
	return c.rw.Write(p)
}

func (c *sniffConn) Close() error {
	if c.rw != nil {
		return c.rw.Close()
	}
	return c.Conn.Close()
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
