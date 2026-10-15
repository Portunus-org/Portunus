package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"strings"

	"github.com/Portunus-org/Portunus/server/internal/core"
	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/qrsvg"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

func (s *Server) routesSetup(mux *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleAdmin, h)) }
	op := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleOperator, h)) }

	// First-run welcome: public address and domain.
	mux.Handle("GET /api/setup", s.requireSession(s.handleSetupState))
	mux.Handle("GET /api/setup/address", admin(s.handleSetupAddress))
	mux.Handle("POST /api/setup/basics", admin(s.handleSetupBasics))
	mux.Handle("POST /api/setup/skip", admin(s.handleSetupSkip))
	mux.Handle("POST /api/setup/guide/done", admin(s.handleGuideDone))

	// Inbound wizard.
	mux.Handle("POST /api/inbounds/suggest", op(s.handleSuggestInbound))
	mux.Handle("POST /api/domain/check", op(s.handleCheckDomain))
	mux.Handle("POST /api/acme/verify-proxy", op(s.handleVerifyProxy))

	// The private CA, to install on devices.
	mux.Handle("GET /api/ca", s.requireSession(s.handleCAInfo))
	mux.Handle("GET /api/ca.crt", s.requireSession(s.handleCACertificate))
}

func (s *Server) handleSetupState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"completed": s.core.SetupCompleted(r.Context()),
		// The rest of the first setup (a connection, a client) is waiting.
		"guide": s.core.GuidePending(r.Context()),
	})
}

func (s *Server) handleSetupAddress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	addr, err := s.core.DetectAddress(ctx)
	host, _ := s.core.PublicHost(ctx)
	domain, _ := s.core.Domain(ctx)
	out := map[string]any{"detected": addr, "publicHost": host, "domain": domain}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetupBasics(w http.ResponseWriter, r *http.Request) {
	var b core.Basics
	if !decodeJSON(w, r, &b) {
		return
	}
	if err := s.core.SaveBasics(r.Context(), b); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.kickSubscriptions()
	s.auditAction(r, "setup.basics", b.PublicHost, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetupSkip(w http.ResponseWriter, r *http.Request) {
	if err := s.core.MarkSetupCompleted(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGuideDone(w http.ResponseWriter, r *http.Request) {
	if err := s.core.EndGuide(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSuggestInbound(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Protocol proxy.Protocol `json:"protocol"`
		Domain   string         `json:"domain"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	sug, err := s.core.SuggestInbound(r.Context(), req.Protocol, req.Domain)
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sug)
}

func (s *Server) handleCheckDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	dc, err := s.core.CheckDomain(r.Context(), req.Domain)
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dc)
}

func (s *Server) handleVerifyProxy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
		Port   int    `json:"port"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Port < 1024 || req.Port > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "port", "code": "out_of_range"})
		return
	}
	if _, err := s.core.CheckDomain(r.Context(), req.Domain); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	if err := s.core.VerifyProxyForwarding(r.Context(), req.Domain, req.Port); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleCACertificate serves the private CA certificate (never the key):
// PEM by default, DER with ?format=der (what Windows and some Android
// versions prefer).
func (s *Server) handleCACertificate(w http.ResponseWriter, r *http.Request) {
	ca, err := s.core.CA(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeCACertificate(w, r, ca)
}

// handleCAInfo describes the private CA and, when the subscription listener
// is on, a public download link (with a QR code) that a phone can open.
func (s *Server) handleCAInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ca, err := s.core.CA(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sum := sha256.Sum256(ca.Cert.Raw)
	out := map[string]any{
		"subject":     ca.Cert.Subject.CommonName,
		"notAfter":    ca.Cert.NotAfter,
		"fingerprint": strings.ToUpper(hex.EncodeToString(sum[:])),
	}
	if base := s.core.EffectiveSubscriptionBase(ctx); base != "" {
		url := strings.TrimSuffix(base, "/") + "/ca.crt"
		out["downloadUrl"] = url
		out["downloadQr"], _ = qrsvg.Encode(url)
	}
	writeJSON(w, http.StatusOK, out)
}

func writeCACertificate(w http.ResponseWriter, r *http.Request, ca *proxy.CA) {
	h := w.Header()
	h.Set("Cache-Control", "no-cache")
	if r.URL.Query().Get("format") == "der" {
		b, _ := pem.Decode([]byte(ca.CertPEM))
		h.Set("Content-Type", "application/pkix-cert")
		h.Set("Content-Disposition", `attachment; filename="portunus-ca.cer"`)
		w.Write(b.Bytes) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- DER certificate (attachment)
		return
	}
	h.Set("Content-Type", "application/x-x509-ca-cert")
	h.Set("Content-Disposition", `attachment; filename="portunus-ca.crt"`)
	w.Write([]byte(ca.CertPEM)) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- PEM certificate (attachment)
}
