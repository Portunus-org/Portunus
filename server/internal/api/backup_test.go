package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Portunus-org/Portunus/server/internal/backup"
)

func TestBackupAndRestoreAPI(t *testing.T) {
	src := newTestEnv(t)
	srcCSRF := src.loginFresh(t)
	srcHdr := map[string]string{csrfHeader: srcCSRF}

	// Configure settings, domain, inbounds, clients, nodes, chains, routing, and cert files on src.
	if resp, body := src.do(t, "PUT", "/api/settings", map[string]any{
		"publicHost":       "vpn.example.com",
		"domain":           "vpn.example.com",
		"subscriptionPort": 2096,
		"subscriptionPath": "sub-custom",
	}, srcHdr); resp.StatusCode != 200 {
		t.Fatalf("put settings: %d %v", resp.StatusCode, body)
	}

	resp, vlessBody := src.do(t, "POST", "/api/inbounds", map[string]any{
		"name":              "Main VLESS",
		"protocol":          "vless",
		"port":              20443,
		"enabled":           true,
		"realityServer":     "www.microsoft.com",
		"realityServerPort": 443,
	}, srcHdr)
	if resp.StatusCode != 201 {
		t.Fatalf("create vless: %d %v", resp.StatusCode, vlessBody)
	}

	resp, hy2Body := src.do(t, "POST", "/api/inbounds", map[string]any{
		"name":       "Fast Hysteria2",
		"protocol":   "hysteria2",
		"port":       20843,
		"enabled":    true,
		"tlsMode":    "self-signed",
		"serverName": "vpn.example.com",
	}, srcHdr)
	if resp.StatusCode != 201 {
		t.Fatalf("create hy2: %d %v", resp.StatusCode, hy2Body)
	}

	resp, nodeBody := src.do(t, "POST", "/api/nodes", map[string]any{
		"kind":    "agent",
		"name":    "Amsterdam",
		"address": "198.51.100.10",
		"isExit":  true,
		"isEntry": true,
		"standby": true,
		"enabled": true,
	}, srcHdr)
	if resp.StatusCode != 201 {
		t.Fatalf("create node: %d %v", resp.StatusCode, nodeBody)
	}
	nodeID := int(nodeBody["id"].(float64))

	var srcJoin map[string]any
	if code := src.getJSON(t, "/api/nodes/1/join", &srcJoin); code != 200 {
		t.Fatalf("src node join: %d", code)
	}

	resp, chainBody := src.do(t, "POST", "/api/chains", map[string]any{
		"name":  "NL Route",
		"edges": []map[string]any{{"from": 0, "to": nodeID}},
	}, srcHdr)
	if resp.StatusCode != 201 {
		t.Fatalf("create chain: %d %v", resp.StatusCode, chainBody)
	}
	chainID := int(chainBody["id"].(float64))

	resp, clientBody := src.do(t, "POST", "/api/clients", map[string]any{
		"name":         "Alice",
		"enabled":      true,
		"trafficLimit": 10737418240,
		"note":         "phone + laptop",
		"chainId":      chainID,
	}, srcHdr)
	if resp.StatusCode != 201 {
		t.Fatalf("create client: %d %v", resp.StatusCode, clientBody)
	}

	var srcAccess accessDTO
	if code := src.getJSON(t, "/api/clients/1/access", &srcAccess); code != 200 || len(srcAccess.Links) == 0 {
		t.Fatalf("src client access: %d %+v", code, srcAccess)
	}

	// Write a certificate file in sing-box/acme and panel-acme on src to verify file preservation.
	acmeFile := filepath.Join(src.api.cfg.DataDir, "sing-box", "acme", "vpn.example.com.crt")
	if err := os.MkdirAll(filepath.Dir(acmeFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acmeFile, []byte("FAKE-ACME-CERT"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Non-admins cannot download or restore backups.
	_, opCreated := src.do(t, "POST", "/api/users", map[string]any{"username": "oleg", "role": "operator"}, srcHdr)
	opJar, _ := cookiejar.New(nil)
	opEnv := &testEnv{srv: src.srv, client: &http.Client{Jar: opJar}, password: opCreated["password"].(string)}
	_, opLogin := opEnv.do(t, "POST", "/api/auth/login", map[string]string{"username": "oleg", "password": opEnv.password}, nil)
	opCSRF := opLogin["csrfToken"].(string)
	opEnv.do(t, "POST", "/api/auth/password", map[string]string{"currentPassword": opEnv.password, "newPassword": "oleg-has-a-long-password"},
		map[string]string{csrfHeader: opCSRF})
	if resp, _ := opEnv.do(t, "GET", "/api/backup", nil, nil); resp.StatusCode != 403 {
		t.Fatalf("operator GET /api/backup: %d", resp.StatusCode)
	}
	if resp, _ := opEnv.do(t, "POST", "/api/backup/restore", nil, map[string]string{csrfHeader: opCSRF}); resp.StatusCode != 403 {
		t.Fatalf("operator POST /api/backup/restore: %d", resp.StatusCode)
	}

	// Download backup from src as admin.
	dlReq, _ := http.NewRequest("GET", src.srv.URL+"/api/backup", nil)
	dlResp, err := src.client.Do(dlReq)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := io.ReadAll(dlResp.Body)
	dlResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if dlResp.StatusCode != 200 {
		t.Fatalf("GET /api/backup: %d", dlResp.StatusCode)
	}
	if ct := dlResp.Header.Get("Content-Type"); ct != "application/gzip" {
		t.Fatalf("Content-Type: %q", ct)
	}
	if disp := dlResp.Header.Get("Content-Disposition"); !strings.Contains(disp, "portunus-backup-") {
		t.Fatalf("Content-Disposition: %q", disp)
	}

	// Spin up a second fresh panel (dst) representing a new VM.
	dst := newTestEnv(t)
	dstCSRF := dst.loginFresh(t)

	postRestore := func(data []byte, key string) (*http.Response, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", dst.srv.URL+"/api/backup/restore", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set(csrfHeader, dstCSRF)
		if key != "" {
			req.Header.Set(recoveryKeyHeader, key)
		}
		res, err := dst.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res, out
	}

	// Invalid archive is rejected without harming dst.
	if res, body := postRestore([]byte("not-a-tar-gz"), ""); res.StatusCode != 400 || body["code"] != "bad_backup" {
		t.Fatalf("corrupt archive: %d %v", res.StatusCode, body)
	}

	// Restore plain backup onto dst.
	res, restoreBody := postRestore(archive, "")
	if res.StatusCode != 200 {
		t.Fatalf("restore backup: %d %v", res.StatusCode, restoreBody)
	}
	newCSRF, _ := restoreBody["csrfToken"].(string)
	if newCSRF == "" {
		t.Fatalf("missing csrfToken after restore: %v", restoreBody)
	}
	dstCSRF = newCSRF

	// Verify dst is still authenticated and all data matches src.
	var dstSettings settingsDTO
	if code := dst.getJSON(t, "/api/settings", &dstSettings); code != 200 {
		t.Fatalf("dst settings: %d", code)
	}
	if dstSettings.Domain != "vpn.example.com" || dstSettings.PublicHost != "vpn.example.com" || dstSettings.SubscriptionPort != 2096 || dstSettings.SubscriptionPath != "sub-custom" {
		t.Fatalf("restored settings mismatch: %+v", dstSettings)
	}

	var dstAccess accessDTO
	if code := dst.getJSON(t, "/api/clients/1/access", &dstAccess); code != 200 {
		t.Fatalf("dst client access: %d", code)
	}
	if dstAccess.SubscriptionURL != srcAccess.SubscriptionURL || len(dstAccess.Links) != len(srcAccess.Links) {
		t.Fatalf("restored links mismatch: got %+v, want %+v", dstAccess, srcAccess)
	}
	for i := range srcAccess.Links {
		if dstAccess.Links[i].URL != srcAccess.Links[i].URL {
			t.Fatalf("link %d mismatch:\ngot:  %s\nwant: %s", i, dstAccess.Links[i].URL, srcAccess.Links[i].URL)
		}
	}

	var dstJoin map[string]any
	if code := dst.getJSON(t, "/api/nodes/1/join", &dstJoin); code != 200 {
		t.Fatalf("dst node join: %d", code)
	}
	if dstJoin["run"] != srcJoin["run"] {
		t.Fatalf("node join command mismatch:\ngot:  %v\nwant: %v", dstJoin["run"], srcJoin["run"])
	}

	gotACME, err := os.ReadFile(filepath.Join(dst.api.cfg.DataDir, "sing-box", "acme", "vpn.example.com.crt"))
	if err != nil || string(gotACME) != "FAKE-ACME-CERT" {
		t.Fatalf("restored acme file: %q, %v", gotACME, err)
	}

	// Test sealed backup (encrypted with a recovery key).
	recKey := backup.NewRecoveryKey()
	sealed, err := backup.Seal(archive, recKey)
	if err != nil {
		t.Fatal(err)
	}
	if res, body := postRestore(sealed, ""); res.StatusCode != 400 || body["code"] != "key_required" {
		t.Fatalf("sealed without key: %d %v", res.StatusCode, body)
	}
	if res, body := postRestore(sealed, "wrong-key"); res.StatusCode != 400 || body["code"] != "wrong_key" {
		t.Fatalf("sealed with wrong key: %d %v", res.StatusCode, body)
	}
	if res, body := postRestore(sealed, recKey); res.StatusCode != 200 {
		t.Fatalf("sealed with valid key: %d %v", res.StatusCode, body)
	}
}
