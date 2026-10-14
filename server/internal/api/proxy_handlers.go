package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PaulichP/Portunus/server/internal/core"
	"github.com/PaulichP/Portunus/server/internal/proxy"
	"github.com/PaulichP/Portunus/server/internal/qrsvg"
	"github.com/PaulichP/Portunus/server/internal/singbox"
	"github.com/PaulichP/Portunus/server/internal/store"
)

var roleRank = map[store.Role]int{store.RoleViewer: 1, store.RoleOperator: 2, store.RoleAdmin: 3}

// requireRole wraps a handler that already runs inside requireSession.
func requireRole(min store.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if roleRank[sessionFrom(r.Context()).User.Role] < roleRank[min] {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	}
}

func (s *Server) routesProxy(mux *http.ServeMux) {
	op := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleOperator, h)) }
	admin := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleAdmin, h)) }
	view := func(h http.HandlerFunc) http.Handler { return s.requireSession(h) }

	mux.Handle("GET /api/inbounds", view(s.handleListInbounds))
	mux.Handle("POST /api/inbounds", op(s.handleCreateInbound))
	mux.Handle("PUT /api/inbounds/{id}", op(s.handleUpdateInbound))
	mux.Handle("DELETE /api/inbounds/{id}", op(s.handleDeleteInbound))
	mux.Handle("POST /api/inbounds/{id}/rotate-keys", op(s.handleRotateInboundKeys))

	mux.Handle("GET /api/clients", view(s.handleListClients))
	mux.Handle("POST /api/clients", op(s.handleCreateClient))
	mux.Handle("PUT /api/clients/{id}", op(s.handleUpdateClient))
	mux.Handle("DELETE /api/clients/{id}", op(s.handleDeleteClient))
	mux.Handle("POST /api/clients/{id}/rotate", op(s.handleRotateClient))
	mux.Handle("POST /api/clients/{id}/reset-traffic", op(s.handleResetTraffic))
	// Links carry credentials: operators only.
	mux.Handle("GET /api/clients/{id}/access", op(s.handleClientAccess))

	mux.Handle("GET /api/settings", view(s.handleGetSettings))
	mux.Handle("PUT /api/settings", admin(s.handlePutSettings))

	mux.Handle("GET /api/singbox", view(s.handleSingBoxStatus))
	mux.Handle("POST /api/singbox/restart", op(s.handleSingBoxRestart))
	mux.Handle("GET /api/singbox/logs", admin(s.handleSingBoxLogs))
	mux.Handle("GET /api/singbox/config", admin(s.handleSingBoxConfig))
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// writeCoreError maps core/store errors to HTTP responses.
func (s *Server) writeCoreError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *core.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": ve.Field, "code": ve.Code})
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	default:
		s.internalError(w, r, err)
	}
}

func (s *Server) auditAction(r *http.Request, action, target string, details map[string]any) {
	u := sessionFrom(r.Context()).User
	if err := s.store.Audit(r.Context(), store.AuditEntry{
		ActorID: u.ID, Actor: u.Username, Action: action, Target: target, IP: clientIP(r), Details: details,
	}); err != nil {
		s.log.Error("write audit log", "action", action, "err", err)
	}
}

// ---------------------------------------------------------------- inbounds

type inboundDTO struct {
	ID                 int64          `json:"id"`
	Tag                string         `json:"tag"`
	Name               string         `json:"name"`
	Protocol           proxy.Protocol `json:"protocol"`
	Port               int            `json:"port"`
	Enabled            bool           `json:"enabled"`
	RealityServer      string         `json:"realityServer,omitempty"`
	RealityServerPort  int            `json:"realityServerPort,omitempty"`
	RealityPublicKey   string         `json:"realityPublicKey,omitempty"`
	RealityShortID     string         `json:"realityShortId,omitempty"`
	ServerName         string         `json:"serverName,omitempty"`
	TLSMode            string         `json:"tlsMode,omitempty"`
	StrictTLS          bool           `json:"strictTls,omitempty"`
	PrivateCertExpires *time.Time     `json:"privateCertExpires,omitempty"`
	// Certificate is the issued ACME certificate (nil while pending).
	Certificate     *core.CertificateInfo `json:"certificate,omitempty"`
	CertificatePath string                `json:"certificatePath,omitempty"`
	KeyPath         string                `json:"keyPath,omitempty"`
	CertSHA256      string                `json:"certSha256,omitempty"`
	MasqueradeURL   string                `json:"masqueradeUrl,omitempty"`
	Obfs            bool                  `json:"obfs,omitempty"`
	Method          string                `json:"method,omitempty"`
}

func (s *Server) toInboundDTO(in proxy.Inbound) inboundDTO {
	d := inboundDTO{
		ID: in.ID, Tag: in.Tag, Name: in.Name, Protocol: in.Protocol, Port: in.Port, Enabled: in.Enabled,
		RealityServer:     in.Settings.RealityServer,
		RealityServerPort: in.Settings.RealityServerPort,
		RealityPublicKey:  in.Settings.RealityPublicKey,
		RealityShortID:    in.Settings.RealityShortID,
		ServerName:        in.Settings.ServerName,
		CertSHA256:        in.Settings.CertSHA256,
		MasqueradeURL:     in.Settings.MasqueradeURL,
		Obfs:              in.Settings.ObfsPassword != "",
		Method:            in.Settings.Method,
	}
	if in.Protocol.UsesTLSCert() {
		d.TLSMode = "self-signed"
		d.StrictTLS = in.Settings.StrictTLS
		if exp, err := proxy.CertExpiry(in.Settings.Certificate); err == nil {
			t := exp.UTC()
			d.PrivateCertExpires = &t
		}
		if in.TrustedCert() {
			d.TLSMode = string(in.Settings.TLSMode)
			d.Certificate = s.core.CertificateFor(in)
			d.CertificatePath, d.KeyPath = in.Settings.CertificatePath, in.Settings.KeyPath
			d.CertSHA256 = ""
		}
	}
	return d
}

type inboundRequest struct {
	Protocol          proxy.Protocol `json:"protocol"`
	Name              string         `json:"name"`
	Port              int            `json:"port"`
	Enabled           *bool          `json:"enabled"`
	RealityServer     string         `json:"realityServer"`
	RealityServerPort int            `json:"realityServerPort"`
	ServerName        string         `json:"serverName"`
	TLSMode           string         `json:"tlsMode"`
	StrictTLS         bool           `json:"strictTls"`
	CertificatePath   string         `json:"certificatePath"`
	KeyPath           string         `json:"keyPath"`
	MasqueradeURL     string         `json:"masqueradeUrl"`
	Obfs              bool           `json:"obfs"`
}

func (req inboundRequest) input() core.InboundInput {
	enabled := req.Enabled == nil || *req.Enabled
	mode := proxy.TLSMode(req.TLSMode)
	if req.TLSMode == "self-signed" {
		mode = proxy.TLSSelfSigned
	}
	return core.InboundInput{
		TLSMode: mode, CertificatePath: req.CertificatePath, KeyPath: req.KeyPath, StrictTLS: req.StrictTLS,
		Protocol: req.Protocol, Name: req.Name, Port: req.Port, Enabled: enabled,
		RealityServer: req.RealityServer, RealityServerPort: req.RealityServerPort, ServerName: req.ServerName,
		MasqueradeURL: req.MasqueradeURL, Obfs: req.Obfs,
	}
}

func (s *Server) handleListInbounds(w http.ResponseWriter, r *http.Request) {
	ins, err := s.core.Inbounds(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]inboundDTO, 0, len(ins))
	for _, in := range ins {
		out = append(out, s.toInboundDTO(in))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateInbound(w http.ResponseWriter, r *http.Request) {
	var req inboundRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := s.core.CreateInbound(r.Context(), req.input())
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "inbound.create", in.Tag, nil)
	writeJSON(w, http.StatusCreated, s.toInboundDTO(in))
}

func (s *Server) handleUpdateInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var req inboundRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := s.core.UpdateInbound(r.Context(), id, req.input())
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "inbound.update", in.Tag, nil)
	writeJSON(w, http.StatusOK, s.toInboundDTO(in))
}

func (s *Server) handleRotateInboundKeys(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	in, err := s.core.RotateInboundKeys(r.Context(), id)
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "inbound.rotate_keys", in.Tag, nil)
	writeJSON(w, http.StatusOK, s.toInboundDTO(in))
}

func (s *Server) handleDeleteInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	in, err := s.core.Inbound(r.Context(), id)
	if err == nil {
		err = s.core.DeleteInbound(r.Context(), id)
	}
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "inbound.delete", in.Tag, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ----------------------------------------------------------------- clients

type clientDTO struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	Status       core.ClientStatus `json:"status"`
	TrafficLimit int64             `json:"trafficLimit"`
	Upload       int64             `json:"upload"`
	Download     int64             `json:"download"`
	ExpiresAt    *time.Time        `json:"expiresAt"`
	Note         string            `json:"note"`
	// ExitID is the exit the client is bound to (null: no exits yet).
	ExitID    *int64    `json:"exitId"`
	CreatedAt time.Time `json:"createdAt"`
	// Online: traffic of the client is being counted right now, at these
	// speeds in bytes per second. LastSeenAt is when it last was (null:
	// never).
	Online     bool       `json:"online"`
	DownRate   int64      `json:"downRate"`
	UpRate     int64      `json:"upRate"`
	LastSeenAt *time.Time `json:"lastSeenAt"`
}

func (s *Server) toClientDTO(c *store.ClientRow) clientDTO {
	var exp *time.Time
	if c.ExpiresAt != nil {
		t := c.ExpiresAt.UTC()
		exp = &t
	}
	var seen *time.Time
	if c.LastSeen != nil {
		t := c.LastSeen.UTC()
		seen = &t
	}
	act := s.core.ClientActivity(c.ID)
	return clientDTO{
		ID: c.ID, Name: c.Name, Enabled: c.Enabled, Status: s.core.ClientStatus(c),
		TrafficLimit: c.TrafficLimit, Upload: c.Upload, Download: c.Download,
		ExpiresAt: exp, Note: c.Note, ExitID: c.ExitID, CreatedAt: c.CreatedAt.UTC(),
		Online: act.Online, DownRate: act.DownRate, UpRate: act.UpRate, LastSeenAt: seen,
	}
}

type clientRequest struct {
	Name         string     `json:"name"`
	Enabled      *bool      `json:"enabled"`
	TrafficLimit int64      `json:"trafficLimit"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	Note         string     `json:"note"`
	// ExitID: absent keeps the exit, 0 has one chosen anew.
	ExitID *int64 `json:"exitId"`
}

func (req clientRequest) input() core.ClientInput {
	return core.ClientInput{
		Name: req.Name, Enabled: req.Enabled == nil || *req.Enabled,
		TrafficLimit: req.TrafficLimit, ExpiresAt: req.ExpiresAt, Note: req.Note, Exit: req.ExitID,
	}
}

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	cs, err := s.core.Clients(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]clientDTO, 0, len(cs))
	for _, c := range cs {
		out = append(out, s.toClientDTO(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	var req clientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := s.core.CreateClient(r.Context(), req.input())
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "client.create", c.Name, nil)
	writeJSON(w, http.StatusCreated, s.toClientDTO(c))
}

func (s *Server) handleUpdateClient(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var req clientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := s.core.UpdateClient(r.Context(), id, req.input())
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "client.update", c.Name, nil)
	writeJSON(w, http.StatusOK, s.toClientDTO(c))
}

// clientMutation handles the body-less POST actions on a client.
func (s *Server) clientMutation(action string, fn func(*http.Request, int64) (*store.ClientRow, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		c, err := fn(r, id)
		if err != nil {
			s.writeCoreError(w, r, err)
			return
		}
		s.auditAction(r, action, c.Name, nil)
		writeJSON(w, http.StatusOK, s.toClientDTO(c))
	}
}

func (s *Server) handleRotateClient(w http.ResponseWriter, r *http.Request) {
	s.clientMutation("client.rotate", func(r *http.Request, id int64) (*store.ClientRow, error) {
		return s.core.RotateClient(r.Context(), id)
	})(w, r)
}

func (s *Server) handleResetTraffic(w http.ResponseWriter, r *http.Request) {
	s.clientMutation("client.reset_traffic", func(r *http.Request, id int64) (*store.ClientRow, error) {
		return s.core.ResetClientTraffic(r.Context(), id)
	})(w, r)
}

func (s *Server) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	c, err := s.core.Client(r.Context(), id)
	if err == nil {
		err = s.core.DeleteClient(r.Context(), id)
	}
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "client.delete", c.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}

type accessLink struct {
	InboundID int64  `json:"inboundId"`
	Label     string `json:"label"`
	Name      string `json:"name"`
	// Host is the server the link leads to: the main one or a backup entry.
	Host     string         `json:"host"`
	Port     int            `json:"port"`
	Protocol proxy.Protocol `json:"protocol"`
	URL      string         `json:"url"`
	QR       string         `json:"qr"`
}

type accessDTO struct {
	PublicHost      string       `json:"publicHost"`
	SubscriptionURL string       `json:"subscriptionUrl,omitempty"`
	SubscriptionQR  string       `json:"subscriptionQr,omitempty"`
	Links           []accessLink `json:"links"`
}

// handleClientAccess returns everything needed to connect a client: share
// links with QR codes and, when configured, the subscription URL.
func (s *Server) handleClientAccess(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	ctx := r.Context()
	c, err := s.core.Client(ctx, id)
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	host, err := s.core.PublicHost(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := accessDTO{PublicHost: host, Links: []accessLink{}}
	if host == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if base := s.core.EffectiveSubscriptionBase(ctx); base != "" {
		out.SubscriptionURL = base + "/sub/" + c.SubToken
		out.SubscriptionQR, _ = qrsvg.Encode(out.SubscriptionURL)
	}
	points, err := s.core.EntryPoints(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	pc := proxy.Client{ID: c.ID, Name: c.Name, UUID: c.UUID, Password: c.Password}
	for _, pt := range points {
		for _, in := range pt.Inbounds {
			if !in.Enabled {
				continue
			}
			link := proxy.ShareLink(pt.Host, in, pc)
			qr, err := qrsvg.Encode(link)
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			out.Links = append(out.Links, accessLink{InboundID: in.ID, Label: in.Label(), Name: in.Name, Host: pt.Host, Port: in.Port, Protocol: in.Protocol, URL: link, QR: qr})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- settings

type settingsDTO struct {
	PublicHost          string `json:"publicHost"`
	SubscriptionBaseURL string `json:"subscriptionBaseUrl"`
	core.ACMESettings
	// SubscriptionListen is read-only (PORTUNUS_SUB_LISTEN).
	SubscriptionListen string `json:"subscriptionListen"`
	Domain             string `json:"domain"`
	SubscriptionPort   int    `json:"subscriptionPort"`
	// SubscriptionURL is the effective public origin of subscription links.
	SubscriptionURL   string `json:"subscriptionUrl"`
	SubscriptionError string `json:"subscriptionError,omitempty"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	host, err := s.core.PublicHost(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	base, err := s.core.SubscriptionBaseURL(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	acme, err := s.core.ACME(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	domain, err := s.core.Domain(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	_, subErr := s.SubscriptionStatus()
	writeJSON(w, http.StatusOK, settingsDTO{
		PublicHost: host, SubscriptionBaseURL: base, SubscriptionListen: s.cfg.SubListen, ACMESettings: acme,
		Domain: domain, SubscriptionPort: s.core.SubscriptionPort(r.Context()),
		SubscriptionURL: s.core.EffectiveSubscriptionBase(r.Context()), SubscriptionError: subErr,
	})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PublicHost          string  `json:"publicHost"`
		SubscriptionBaseURL string  `json:"subscriptionBaseUrl"`
		ACMEEmail           *string `json:"acmeEmail"`
		ACMEChallenge       *string `json:"acmeChallenge"`
		ACMEHTTPPort        int     `json:"acmeHttpPort"`
		// CloudflareToken: omitted keeps the stored token.
		CloudflareToken  *string `json:"cloudflareToken"`
		Domain           *string `json:"domain"`
		SubscriptionPort *int    `json:"subscriptionPort"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	// The page saves these together, so they are saved together: one field
	// that is refused refuses the request, and none of the others is kept.
	err := s.core.AllOrNothing(ctx, func() error {
		if req.ACMEChallenge != nil {
			email := ""
			if req.ACMEEmail != nil {
				email = *req.ACMEEmail
			}
			if err := s.core.SetACME(ctx, core.ACMESettings{Email: email, Challenge: *req.ACMEChallenge, HTTPPort: req.ACMEHTTPPort, CloudflareToken: req.CloudflareToken}); err != nil {
				return err
			}
		}
		if err := s.core.SetPublicHost(ctx, req.PublicHost); err != nil {
			return err
		}
		if err := s.core.SetSubscriptionBaseURL(ctx, req.SubscriptionBaseURL); err != nil {
			return err
		}
		if req.Domain != nil {
			if err := s.core.SetDomain(ctx, *req.Domain); err != nil {
				return err
			}
		}
		if req.SubscriptionPort != nil {
			return s.core.SetSubscriptionPort(ctx, *req.SubscriptionPort)
		}
		return nil
	})
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	if req.SubscriptionPort != nil {
		s.kickSubscriptions()
	}
	s.auditAction(r, "settings.update", "", nil)
	s.handleGetSettings(w, r)
}

// ---------------------------------------------------------------- sing-box

func (s *Server) handleSingBoxStatus(w http.ResponseWriter, r *http.Request) {
	st := s.mgr.Status()
	var uptime int64
	if st.State == singbox.StateRunning {
		uptime = int64(time.Since(st.StartedAt).Seconds())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        st,
		"uptimeSeconds": uptime,
		"applyError":    s.core.ApplyError(),
	})
}

func (s *Server) handleSingBoxRestart(w http.ResponseWriter, r *http.Request) {
	if err := s.core.RestartSingBox(r.Context()); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_running", "message": err.Error()})
		return
	}
	s.auditAction(r, "singbox.restart", "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSingBoxLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"lines": s.mgr.Logs()})
}

// handleSingBoxConfig shows the running config with credentials and private
// keys masked; it is for inspection, not for copying elsewhere.
func (s *Server) handleSingBoxConfig(w http.ResponseWriter, r *http.Request) {
	raw := s.mgr.Config()
	if raw == nil {
		writeJSON(w, http.StatusOK, map[string]any{"config": nil})
		return
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": redact(v)})
}

var redactedKeys = map[string]bool{"private_key": true, "key": true, "password": true, "uuid": true, "secret": true}

func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if redactedKeys[k] {
				t[k] = "<redacted>"
				continue
			}
			if k == "certificate" {
				t[k] = "<certificate>"
				continue
			}
			t[k] = redact(val)
		}
	case []any:
		for i := range t {
			t[i] = redact(t[i])
		}
	}
	return v
}

// ------------------------------------------------------------ subscription

// handleSubscription serves a client's subscription. The token in the path is
// the only credential, so unknown tokens and clients that may not connect
// (disabled, expired, over their traffic limit) get the same plain 404.
//
// Format: sing-box JSON profile for sing-box apps (detected by User-Agent or
// ?format=singbox), otherwise a base64 list of share links, which v2rayN,
// Hiddify, NekoBox, Streisand and others import.
func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	notFound := func() { http.Error(w, "404 page not found", http.StatusNotFound) }

	c, err := s.core.ClientBySubToken(ctx, r.PathValue("token"))
	// Only a client that may connect gets anything: one that is switched
	// off, out of time or over its traffic limit is cut off here as well.
	if err != nil || s.core.ClientStatus(c) != core.ClientActive {
		notFound()
		return
	}
	host, err := s.core.PublicHost(ctx)
	if err != nil || host == "" {
		notFound()
		return
	}
	points, err := s.core.EntryPoints(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	pc := proxy.Client{ID: c.ID, Name: c.Name, UUID: c.UUID, Password: c.Password}

	h := w.Header()
	var expire int64
	if c.ExpiresAt != nil {
		expire = c.ExpiresAt.Unix()
	}
	h.Set("Subscription-Userinfo", "upload="+strconv.FormatInt(c.Upload, 10)+
		"; download="+strconv.FormatInt(c.Download, 10)+
		"; total="+strconv.FormatInt(c.TrafficLimit, 10)+
		"; expire="+strconv.FormatInt(expire, 10))
	h.Set("Profile-Update-Interval", "12")
	h.Set("Profile-Title", "Portunus")
	h.Set("Cache-Control", "no-store")

	format := r.URL.Query().Get("format")
	if format == "" && isSingBoxClient(r.UserAgent()) {
		format = "singbox"
	}
	if format == "singbox" {
		cfg, err := proxy.ClientProfile(points, pc)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		h.Set("Content-Type", "application/json")
		h.Set("Content-Disposition", `attachment; filename="portunus.json"`)
		w.Write(cfg) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- JSON download (attachment), not HTML
		return
	}

	var links []string
	for _, pt := range points {
		for _, in := range pt.Inbounds {
			if in.Enabled {
				links = append(links, proxy.ShareLink(pt.Host, in, pc))
			}
		}
	}
	h.Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(base64Std(strings.Join(links, "\n")))) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- base64 text/plain
}

func isSingBoxClient(ua string) bool {
	ua = strings.ToLower(ua)
	for _, p := range []string{"sing-box", "sfa/", "sfi/", "sfm/", "sft/"} {
		if strings.Contains(ua, p) {
			return true
		}
	}
	return false
}
