package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/store"
)

// maxBackupBytes bounds the size of an uploaded backup archive (64 MiB).
const maxBackupBytes = 64 << 20

const recoveryKeyHeader = "X-Recovery-Key"

func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := s.core.WriteBackup(r.Context(), &buf); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.auditAction(r, "backup.download", "", nil)
	filename := fmt.Sprintf("portunus-backup-%s.tgz", time.Now().UTC().Format("2006-01-02"))
	h := w.Header()
	h.Set("Content-Type", "application/gzip")
	h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes()) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- gzip archive download with attachment disposition
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupBytes)
	var (
		archive     []byte
		recoveryKey string
		err         error
	)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(maxBackupBytes); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "file", "code": "too_large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "file", "code": "bad_backup"})
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		recoveryKey = r.FormValue("key")
		f, _, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "file", "code": "bad_backup"})
			return
		}
		defer f.Close()
		archive, err = io.ReadAll(f)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "file", "code": "bad_backup"})
			return
		}
	} else {
		recoveryKey = r.Header.Get(recoveryKeyHeader)
		archive, err = io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "file", "code": "too_large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "file", "code": "bad_backup"})
			return
		}
	}

	prevActor := sessionFrom(r.Context()).User.Username
	if err := s.core.RestoreBackup(r.Context(), archive, recoveryKey); err != nil {
		s.writeCoreError(w, r, err)
		return
	}

	s.panel.mu.Lock()
	s.panel.cover = nil
	s.panel.mu.Unlock()
	s.kickSubscriptions()
	s.kickPanelAccess()

	users, err := s.store.Users(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	adminUser := pickRestoredAdmin(users, prevActor)
	if adminUser == nil {
		s.internalError(w, r, errors.New("no active admin user after restore"))
		return
	}
	sess, err := s.auth.OpenSession(r.Context(), adminUser, clientIP(r), r.UserAgent())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.setSessionCookie(w, r, sess)
	if err := s.store.Audit(r.Context(), store.AuditEntry{
		ActorID: adminUser.ID, Actor: adminUser.Username, Action: "backup.restore", IP: clientIP(r),
	}); err != nil {
		s.log.Error("write audit log", "action", "backup.restore", "err", err)
	}
	writeJSON(w, http.StatusOK, toSessionDTO(sess))
}

func pickRestoredAdmin(users []*store.User, preferred string) *store.User {
	var fallback, adminNamed *store.User
	for _, u := range users {
		if u.Role != store.RoleAdmin || u.Disabled {
			continue
		}
		if strings.EqualFold(u.Username, preferred) {
			return u
		}
		if strings.EqualFold(u.Username, "admin") && adminNamed == nil {
			adminNamed = u
		}
		if fallback == nil {
			fallback = u
		}
	}
	if adminNamed != nil {
		return adminNamed
	}
	return fallback
}
