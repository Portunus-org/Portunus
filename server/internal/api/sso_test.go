package api

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/PaulichP/Portunus/server/internal/core"
	"github.com/PaulichP/Portunus/server/internal/store"
)

// idp is an OIDC provider as the real ones behave: it describes itself,
// publishes its keys, hands a one-time code to whoever signs in, and trades
// the code for a signed ID token only when the client, the redirect address
// and the PKCE verifier are the ones the code was given for.
type idp struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	// Who signs in next.
	subject, username, name string
	groups                  any

	// What a dishonest or broken provider would do.
	wrongNonce, wrongAudience, wrongIssuer, expired, foreignKey bool
	// refuse makes the provider answer the sign-in with an error.
	refuse bool

	codes     map[string]grant
	exchanges int
}

type grant struct{ challenge, nonce, redirect, client string }

const (
	idpClient = "portunus"
	idpSecret = "a-client-secret"
)

func newIDP(t *testing.T) *idp {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &idp{t: t, key: key, subject: "sub-anna", username: "anna", name: "Anna Lindqvist", groups: []string{"portunus-admins"}, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/authorize", "token_endpoint": p.srv.URL + "/token",
			"jwks_uri": p.srv.URL + "/jwks", "userinfo_endpoint": p.srv.URL + "/userinfo",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		back, _ := url.Parse(q.Get("redirect_uri"))
		out := url.Values{"state": {q.Get("state")}}
		if p.refuse {
			out.Set("error", "access_denied")
		} else {
			if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("nonce") == "" || !strings.Contains(q.Get("scope"), "openid") {
				t.Errorf("the panel asked the provider wrongly: %s", r.URL.RawQuery)
			}
			code := base64.RawURLEncoding.EncodeToString(randomBytes(16))
			p.codes[code] = grant{challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), redirect: q.Get("redirect_uri"), client: q.Get("client_id")}
			out.Set("code", code)
		}
		back.RawQuery = out.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		p.exchanges++
		r.ParseForm()
		id, secret, basic := r.BasicAuth()
		if !basic {
			id, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
		}
		g, ok := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code")) // a code is good once
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || id != idpClient || secret != idpSecret || g.client != id || g.redirect != r.Form.Get("redirect_uri") ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		now := time.Now()
		claims := map[string]any{
			"iss": p.srv.URL, "aud": idpClient, "sub": p.subject, "nonce": g.nonce,
			"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
			"preferred_username": p.username, "name": p.name,
		}
		if p.groups != nil {
			claims["groups"] = p.groups
		}
		switch {
		case p.wrongNonce:
			claims["nonce"] = "the-nonce-of-another-sign-in"
		case p.wrongAudience:
			claims["aud"] = "another-application"
		case p.wrongIssuer:
			claims["iss"] = "https://another-provider.example"
		case p.expired:
			claims["exp"] = now.Add(-time.Hour).Unix()
		}
		key := p.key
		if p.foreignKey {
			key, _ = rsa.GenerateKey(rand.Reader, 2048)
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(claims)
		jws, err := signer.Sign(raw)
		if err != nil {
			t.Fatal(err)
		}
		token, _ := jws.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 300, "id_token": token})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sub": p.subject})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// browser is someone's browser: its own cookies, and it follows redirects.
func (e *testEnv) browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// signIn goes through the provider and reports where the browser ended up
// and what it was shown.
func (e *testEnv) signIn(t *testing.T, b *http.Client) (final *url.URL, body string) {
	t.Helper()
	res, err := b.Get(e.srv.URL + "/api/auth/sso/start")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.Request.URL, string(raw)
}

func (e *testEnv) whoami(t *testing.T, b *http.Client) (int, map[string]any) {
	t.Helper()
	res, err := b.Get(e.srv.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		User map[string]any `json:"user"`
	}
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out.User
}

func enableSSO(t *testing.T, e *testEnv, p *idp, change func(*core.SSO)) {
	t.Helper()
	s := core.SSO{Enabled: true, Name: "Pocket ID", Issuer: p.srv.URL, ClientID: idpClient,
		AdminGroup: "portunus-admins", OperatorGroup: "portunus-operators", ViewerGroup: "portunus-viewers"}
	if change != nil {
		change(&s)
	}
	secret := idpSecret
	if err := e.core.SetSSO(t.Context(), s, &secret); err != nil {
		t.Fatal(err)
	}
}

func TestSignInThroughAProvider(t *testing.T) {
	e := newTestEnv(t)
	p := newIDP(t)

	// Off: the page offers nothing and the start leads back to sign-in.
	var methods map[string]any
	e.getJSON(t, "/api/auth/methods", &methods)
	if _, offered := methods["sso"]; offered {
		t.Fatal("a provider is offered before one is set up")
	}
	if at, _ := e.signIn(t, e.browser()); at.Query().Get("sso") != "off" {
		t.Fatalf("sign-in started with no provider: %s", at)
	}

	enableSSO(t, e, p, nil)
	e.getJSON(t, "/api/auth/methods", &methods)
	if sso, _ := methods["sso"].(map[string]any); sso["name"] != "Pocket ID" || len(sso) != 1 {
		t.Fatalf("what the sign-in page is told: %v", methods)
	}

	// Anna is in the administrators' group.
	anna := e.browser()
	at, page := e.signIn(t, anna)
	if at.Path != "/api/auth/sso/callback" || !strings.Contains(page, `http-equiv="refresh"`) || !strings.Contains(page, `url=/"`) {
		t.Fatalf("after signing in: %s\n%s", at, page)
	}
	code, me := e.whoami(t, anna)
	if code != 200 || me["username"] != "anna" || me["role"] != "admin" || me["mustChangePassword"] != false {
		t.Fatalf("who the panel thinks signed in: %d %v", code, me)
	}
	u, err := e.store.UserByIdentity(t.Context(), p.srv.URL, "sub-anna")
	if err != nil || u.PasswordHash != "" || u.DisplayName != "Anna Lindqvist" || u.LastLoginAt.IsZero() {
		t.Fatalf("the account: %+v %v", u, err)
	}
	// The account has no password: its name and any password get nowhere.
	if resp, _ := e.do(t, "POST", "/api/auth/login", map[string]string{"username": "anna", "password": "anything-at-all-1234"}, nil); resp.StatusCode != 401 {
		t.Fatalf("a provider account signed in with a password: %d", resp.StatusCode)
	}

	// Her groups change at the provider: so does what she may do here, at
	// the next sign-in, on the same account.
	p.groups = []string{"staff", "portunus-viewers"}
	again := e.browser()
	e.signIn(t, again)
	if _, me := e.whoami(t, again); me["role"] != "viewer" || me["id"] != float64(u.ID) {
		t.Fatalf("after her groups changed: %v", me)
	}
	// And the session she already had follows: roles are read at each request.
	if _, me := e.whoami(t, anna); me["role"] != "viewer" {
		t.Fatalf("her earlier session kept the old role: %v", me)
	}

	// Someone in no group that gives access is not let in, and gets no account.
	p.subject, p.username, p.groups = "sub-boris", "boris", []string{"staff"}
	boris := e.browser()
	if at, _ := e.signIn(t, boris); at.Path != "/login/" || at.Query().Get("sso") != "denied" {
		t.Fatalf("someone without access: %s", at)
	}
	if code, _ := e.whoami(t, boris); code != 401 {
		t.Fatal("someone without access has a session")
	}
	if _, err := e.store.UserByIdentity(t.Context(), p.srv.URL, "sub-boris"); err == nil {
		t.Fatal("an account was made for someone who was not let in")
	}
	// With a default role he is let in with that role.
	enableSSO(t, e, p, func(s *core.SSO) { s.DefaultRole = "viewer" })
	e.signIn(t, boris)
	if _, me := e.whoami(t, boris); me["role"] != "viewer" || me["username"] != "boris" {
		t.Fatalf("with a default role: %v", me)
	}

	// The provider calls someone "admin". That is not the local
	// administrator: the identity gets an account of its own.
	p.subject, p.username, p.groups = "sub-eve", "admin", []string{"portunus-viewers"}
	eve := e.browser()
	e.signIn(t, eve)
	_, me = e.whoami(t, eve)
	if me["username"] != "admin" || me["role"] != "viewer" || me["sso"] != true {
		t.Fatalf("an identity named like the local administrator: %v", me)
	}
	// The name is the provider's: changed there, it is changed here at the
	// next sign-in, on the same account.
	eveID := me["id"]
	p.username, p.name = "eve.the.second", "Eve Marek"
	e.signIn(t, eve)
	if _, me := e.whoami(t, eve); me["username"] != "eve.the.second" || me["displayName"] != "Eve Marek" || me["id"] != eveID {
		t.Fatalf("after the provider renamed her: %v", me)
	}
	// A provider that says nothing but the subject.
	p.subject, p.username, p.name = "sub-0123456789abcdef-nameless", "", ""
	nameless := e.browser()
	e.signIn(t, nameless)
	if _, me := e.whoami(t, nameless); me["username"] != "sub-0123456789ab" {
		t.Fatalf("someone the provider gives no name: %v", me)
	}
	p.name = "Anna Lindqvist"
	local, _ := e.store.UserByUsername(t.Context(), "admin")
	if local.OIDCSubject != "" || local.Role != store.RoleAdmin || local.PasswordHash == "" {
		t.Fatalf("the local administrator was touched: %+v", local)
	}
	// And the password of the local "admin" does not open the other one.
	if resp, who := e.do(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": e.password}, nil); resp.StatusCode != 200 || who["user"].(map[string]any)["sso"] == true {
		t.Fatalf("signing in as the local administrator: %d %v", resp.StatusCode, who)
	}

	// Groups written as one string, and in a nested claim.
	p.subject, p.username, p.groups = "sub-clara", "clara", "staff portunus-operators"
	clara := e.browser()
	e.signIn(t, clara)
	if _, me := e.whoami(t, clara); me["role"] != "operator" {
		t.Fatalf("groups given as one string: %v", me)
	}
	enableSSO(t, e, p, func(s *core.SSO) { s.GroupsClaim = "groups.roles" })
	p.subject, p.username, p.groups = "sub-dan", "dan", map[string]any{"roles": []string{"portunus-admins"}}
	dan := e.browser()
	e.signIn(t, dan)
	if _, me := e.whoami(t, dan); me["role"] != "admin" {
		t.Fatalf("groups in a nested claim: %v", me)
	}

	// An account that was disabled here stays out, whatever the provider says.
	enableSSO(t, e, p, nil)
	cu, _ := e.store.UserByIdentity(t.Context(), p.srv.URL, "sub-clara")
	e.store.SetUserAccess(t.Context(), cu.ID, cu.Role, true)
	p.subject, p.username, p.groups = "sub-clara", "clara", []string{"portunus-admins"}
	if at, _ := e.signIn(t, e.browser()); at.Query().Get("sso") != "denied" {
		t.Fatalf("a disabled account: %s", at)
	}
}

// Everything that is not an honest answer to this browser's own attempt
// ends without a session.
func TestSignInThatDoesNotCheckOut(t *testing.T) {
	e := newTestEnv(t)
	p := newIDP(t)
	enableSSO(t, e, p, nil)
	users := func() int { n, _ := e.store.CountUsers(t.Context()); return n }

	noSession := func(what string, b *http.Client, at *url.URL, want string) {
		t.Helper()
		if at.Path != "/login/" || at.Query().Get("sso") != want {
			t.Errorf("%s: ended at %s, want sso=%s", what, at, want)
		}
		if code, _ := e.whoami(t, b); code != 401 {
			t.Errorf("%s: a session was opened", what)
		}
	}

	// What a broken or dishonest provider can send.
	for what, set := range map[string]func(){
		"an ID token for another sign-in":   func() { p.wrongNonce = true },
		"an ID token for another app":       func() { p.wrongAudience = true },
		"an ID token from another provider": func() { p.wrongIssuer = true },
		"an expired ID token":               func() { p.expired = true },
		"an ID token signed by another key": func() { p.foreignKey = true },
	} {
		set()
		b := e.browser()
		at, _ := e.signIn(t, b)
		noSession(what, b, at, "failed")
		p.wrongNonce, p.wrongAudience, p.wrongIssuer, p.expired, p.foreignKey = false, false, false, false, false
	}
	// The person says no at the provider.
	p.refuse = true
	b := e.browser()
	at, _ := e.signIn(t, b)
	noSession("refused at the provider", b, at, "refused")
	p.refuse = false

	// By hand from here: the steps a browser makes, one at a time.
	steps := func() (*http.Client, *url.URL) {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := c.Get(e.srv.URL + "/api/auth/sso/start")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		to, _ := url.Parse(res.Header.Get("Location"))
		for _, ck := range res.Cookies() {
			if ck.Name == ssoFlowCookie && (!ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != ssoPath || strings.Contains(ck.Value, to.Query().Get("state"))) {
				t.Errorf("the cookie that holds the attempt: %+v", ck)
			}
		}
		// At the provider.
		res, err = c.Get(to.String())
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		back, _ := url.Parse(res.Header.Get("Location"))
		return c, back
	}
	get := func(c *http.Client, u string) *url.URL {
		res, err := c.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		to, _ := url.Parse(res.Header.Get("Location"))
		if to == nil || res.StatusCode == 200 {
			return &url.URL{Path: "(signed in)"}
		}
		return to
	}

	// A link to the callback, sent to someone who never started: the code
	// is the attacker's own, and would sign the victim in as the attacker.
	_, back := steps()
	victim := e.browser()
	victim.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	noSession("a callback link opened by someone else", victim, get(victim, back.String()), "expired")

	// The right browser, but the answer was tampered with on the way.
	c, back := steps()
	q := back.Query()
	q.Set("state", "another-state")
	back.RawQuery = q.Encode()
	before := p.exchanges
	noSession("a changed state", c, get(c, back.String()), "expired")
	if p.exchanges != before {
		t.Error("a code was taken to the provider though the state did not match")
	}

	// A code from another attempt, in this browser's attempt.
	_, stolen := steps()
	c, back = steps()
	q = back.Query()
	q.Set("code", stolen.Query().Get("code"))
	back.RawQuery = q.Encode()
	noSession("somebody else's code", c, get(c, back.String()), "failed")

	// The same answer twice: the attempt is used up by the first.
	c, back = steps()
	if at := get(c, back.String()); at.Path != "(signed in)" {
		t.Fatalf("an honest sign-in by hand: %s", at)
	}
	replay := e.browser()
	replay.CheckRedirect = c.CheckRedirect
	noSession("the same answer again, elsewhere", replay, get(replay, back.String()), "expired")
	if at := get(c, back.String()); at.Query().Get("sso") != "expired" {
		t.Errorf("the same answer again, in the same browser: %s", at)
	}

	// A cookie made up by the person.
	forged := e.browser()
	forged.CheckRedirect = c.CheckRedirect
	base, _ := url.Parse(e.srv.URL)
	forged.Jar.SetCookies(base.JoinPath(ssoPath), []*http.Cookie{{Name: ssoFlowCookie, Value: `{"s":"x","n":"y","v":"z"}`, Path: ssoPath}})
	noSession("a made-up cookie", forged, get(forged, e.srv.URL+ssoPath+"/callback?state=x&code=whatever"), "expired")

	if n := users(); n != 2 {
		t.Fatalf("%d accounts after all this: only the administrator and the one honest sign-in should exist", n)
	}
}

func TestProviderSettings(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	p := newIDP(t)
	good := map[string]any{"enabled": true, "name": "Pocket ID", "issuer": p.srv.URL, "clientId": idpClient, "clientSecret": idpSecret,
		"scopes": "openid profile email groups", "groupsClaim": "groups", "adminGroup": "portunus-admins", "operatorGroup": "", "viewerGroup": "", "defaultRole": ""}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for a, b := range good {
			m[a] = b
		}
		m[k] = v
		return m
	}
	for what, c := range map[string]struct {
		body  map[string]any
		field string
	}{
		"a provider over plain HTTP":  {with("issuer", "http://idp.example.com"), "issuer"},
		"an address with a login":     {with("issuer", "https://user:pw@idp.example.com"), "issuer"},
		"something that is not a URL": {with("issuer", "idp"), "issuer"},
		"no client":                   {with("clientId", ""), "clientId"},
		"scopes without openid":       {with("scopes", "profile email"), "scopes"},
		"a role that does not exist":  {with("defaultRole", "root"), "defaultRole"},
		"settings that let nobody in": {with("adminGroup", ""), "defaultRole"},
	} {
		if resp, body := e.do(t, "PUT", "/api/sso", c.body, h); resp.StatusCode != 400 || body["field"] != c.field {
			t.Errorf("%s: %d %v", what, resp.StatusCode, body)
		}
	}
	resp, body := e.do(t, "PUT", "/api/sso", good, h)
	if resp.StatusCode != 200 {
		t.Fatalf("save: %d %v", resp.StatusCode, body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), idpSecret) {
		t.Fatal("the client secret came back in the answer")
	}
	set := body["settings"].(map[string]any)
	if set["hasSecret"] != true || !strings.HasSuffix(body["callback"].(string), "/api/auth/sso/callback") {
		t.Fatalf("what is shown: %v", body)
	}
	// In the database the secret is not in the clear.
	if stored, _ := e.store.Setting(t.Context(), "sso_client_secret"); stored == "" || strings.Contains(stored, idpSecret) {
		t.Fatalf("the secret as stored: %q", stored)
	}
	// Saved again without the secret, it is kept, and signing in works.
	delete(good, "clientSecret")
	if resp, _ := e.do(t, "PUT", "/api/sso", good, h); resp.StatusCode != 200 {
		t.Fatal("save without the secret")
	}
	b := e.browser()
	e.signIn(t, b)
	if code, me := e.whoami(t, b); code != 200 || me["role"] != "admin" {
		t.Fatalf("after saving through the API: %d %v", code, me)
	}

	// The check button.
	if _, body := e.do(t, "POST", "/api/sso/test", map[string]string{"issuer": p.srv.URL}, h); body["ok"] != true {
		t.Fatalf("a provider that is there: %v", body)
	}
	if _, body := e.do(t, "POST", "/api/sso/test", map[string]string{"issuer": "http://127.0.0.1:1"}, h); body["ok"] != false || body["error"] != nil {
		t.Fatalf("a provider that is not there: %v", body)
	}
	if _, body := e.do(t, "POST", "/api/sso/test", map[string]string{"issuer": "http://169.254.169.254"}, h); body["ok"] != false {
		t.Fatalf("an address that is not a provider: %v", body)
	}

	// Not for anyone but an administrator, and not without signing in.
	if code := e.getJSON(t, "/api/sso", nil); code != 200 {
		t.Fatalf("the administrator reads the settings: %d", code)
	}
	stranger := e.browser()
	for _, path := range []string{"/api/sso", "/api/users"} {
		res, _ := stranger.Get(e.srv.URL + path)
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Errorf("%s without signing in: %d", path, res.StatusCode)
		}
	}
	// The viewer who signed in through the provider.
	p.groups = []string{"nobody"}
	e.do(t, "PUT", "/api/sso", with("defaultRole", "viewer"), h)
	p.subject, p.username = "sub-viewer", "vera"
	viewer := e.browser()
	e.signIn(t, viewer)
	for _, path := range []string{"/api/sso", "/api/users"} {
		res, _ := viewer.Get(e.srv.URL + path)
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Errorf("%s as a viewer: %d", path, res.StatusCode)
		}
	}
}

func TestAccounts(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	accounts := func() []map[string]any { return e.list(t, "/api/users") }

	all := accounts()
	if len(all) != 1 || all[0]["username"] != "admin" || all[0]["self"] != true || all[0]["sso"] != false {
		t.Fatalf("at the start: %v", all)
	}
	me := int(all[0]["id"].(float64))
	at := func(id int) string { return "/api/users/" + strconv.Itoa(id) }

	// The only administrator cannot lock the panel: not by a role, not by
	// disabling, not by deleting, and not their own account in any case.
	for what, do := range map[string]func() (*http.Response, map[string]any){
		"demote": func() (*http.Response, map[string]any) {
			return e.do(t, "PUT", at(me), map[string]any{"role": "viewer", "disabled": false}, h)
		},
		"disable": func() (*http.Response, map[string]any) {
			return e.do(t, "PUT", at(me), map[string]any{"role": "admin", "disabled": true}, h)
		},
		"delete": func() (*http.Response, map[string]any) { return e.do(t, "DELETE", at(me), nil, h) },
		"reset": func() (*http.Response, map[string]any) {
			return e.do(t, "POST", at(me)+"/reset-password", map[string]any{}, h)
		},
	} {
		if resp, body := do(); resp.StatusCode != 400 || body["code"] != "own_account" {
			t.Errorf("%s one's own account: %d %v", what, resp.StatusCode, body)
		}
	}

	for what, body := range map[string]map[string]any{
		"a name with a slash": {"username": "../admin", "role": "viewer"},
		"an empty name":       {"username": " ", "role": "viewer"},
		"a taken name":        {"username": "Admin", "role": "viewer"},
		"no such role":        {"username": "olga", "role": "owner"},
	} {
		if resp, _ := e.do(t, "POST", "/api/users", body, h); resp.StatusCode != 400 {
			t.Errorf("%s: %d", what, resp.StatusCode)
		}
	}
	resp, made := e.do(t, "POST", "/api/users", map[string]any{"username": "olga", "role": "operator"}, h)
	password, _ := made["password"].(string)
	if resp.StatusCode != 201 || len(password) < 12 {
		t.Fatalf("create: %d %v", resp.StatusCode, made)
	}
	olgaID := int(made["user"].(map[string]any)["id"].(float64))
	// The password is told once and not kept anywhere it can be read from.
	raw, _ := json.Marshal(accounts())
	if strings.Contains(string(raw), password) {
		t.Fatal("the list of accounts carries a password")
	}

	// Olga signs in, has to choose her own password first, and is an operator.
	olga := &testEnv{srv: e.srv, client: e.browser()}
	_, sess := olga.do(t, "POST", "/api/auth/login", map[string]string{"username": "olga", "password": password}, nil)
	oh := map[string]string{csrfHeader: sess["csrfToken"].(string)}
	if code := olga.getJSON(t, "/api/clients", nil); code != 403 {
		t.Fatalf("before choosing a password: %d", code)
	}
	if resp, _ := olga.do(t, "POST", "/api/auth/password", map[string]string{"currentPassword": password, "newPassword": "olga-chose-this-one"}, oh); resp.StatusCode != 204 {
		t.Fatalf("choosing a password: %d", resp.StatusCode)
	}
	if code := olga.getJSON(t, "/api/clients", nil); code != 200 {
		t.Fatalf("an operator reads clients: %d", code)
	}
	if code := olga.getJSON(t, "/api/users", nil); code != 403 {
		t.Fatalf("an operator reads accounts: %d", code)
	}
	if resp, _ := olga.do(t, "POST", "/api/users", map[string]any{"username": "mallory", "role": "admin"}, oh); resp.StatusCode != 403 {
		t.Fatalf("an operator makes an administrator: %d", resp.StatusCode)
	}
	if resp, _ := olga.do(t, "PUT", at(olgaID), map[string]any{"role": "admin", "disabled": false}, oh); resp.StatusCode != 403 {
		t.Fatalf("an operator makes herself an administrator: %d", resp.StatusCode)
	}

	// Made a viewer: what she may do changes at once, in the session she has.
	if resp, _ := e.do(t, "PUT", at(olgaID), map[string]any{"role": "viewer", "disabled": false}, h); resp.StatusCode != 200 {
		t.Fatalf("change a role: %d", resp.StatusCode)
	}
	if resp, _ := olga.do(t, "POST", "/api/clients", map[string]any{"name": "x"}, oh); resp.StatusCode != 403 {
		t.Fatalf("a viewer creates a client: %d", resp.StatusCode)
	}
	// A new password: the old one and her session stop working.
	resp, reset := e.do(t, "POST", at(olgaID)+"/reset-password", map[string]any{}, h)
	if resp.StatusCode != 200 || reset["password"] == "" || reset["password"] == password {
		t.Fatalf("reset: %d %v", resp.StatusCode, reset)
	}
	if code := olga.getJSON(t, "/api/clients", nil); code != 401 {
		t.Fatalf("her session after a reset: %d", code)
	}
	if resp, _ := olga.do(t, "POST", "/api/auth/login", map[string]string{"username": "olga", "password": "olga-chose-this-one"}, nil); resp.StatusCode != 401 {
		t.Fatalf("her old password after a reset: %d", resp.StatusCode)
	}
	// Disabled: no way in.
	e.do(t, "PUT", at(olgaID), map[string]any{"role": "viewer", "disabled": true}, h)
	if resp, _ := olga.do(t, "POST", "/api/auth/login", map[string]string{"username": "olga", "password": reset["password"].(string)}, nil); resp.StatusCode != 401 {
		t.Fatalf("a disabled account signs in: %d", resp.StatusCode)
	}

	// A second administrator may be demoted; then the first is the last again.
	_, second := e.do(t, "POST", "/api/users", map[string]any{"username": "petr", "role": "admin"}, h)
	petr := int(second["user"].(map[string]any)["id"].(float64))
	if resp, _ := e.do(t, "PUT", at(petr), map[string]any{"role": "operator", "disabled": false}, h); resp.StatusCode != 200 {
		t.Fatalf("demote the second administrator: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "DELETE", at(olgaID), nil, h); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "DELETE", at(olgaID), nil, h); resp.StatusCode != 404 {
		t.Fatalf("delete again: %d", resp.StatusCode)
	}
	if n := len(accounts()); n != 2 {
		t.Fatalf("%d accounts in the end", n)
	}

	// An administrator who comes from the provider does not make the one
	// with a password dispensable: the provider can take that role back, or
	// stop answering, at any moment.
	idp := newIDP(t)
	enableSSO(t, e, idp, nil)
	anna := e.browser()
	e.signIn(t, anna)
	if _, who := e.whoami(t, anna); who["role"] != "admin" {
		t.Fatalf("the administrator from the provider: %v", who)
	}
	annaEnv := &testEnv{srv: e.srv, client: anna}
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	annaEnv.getJSON(t, "/api/auth/me", &session)
	ah := map[string]string{csrfHeader: session.CSRFToken}
	for what, do := range map[string]func() (*http.Response, map[string]any){
		"demote": func() (*http.Response, map[string]any) {
			return annaEnv.do(t, "PUT", at(me), map[string]any{"role": "viewer", "disabled": false}, ah)
		},
		"disable": func() (*http.Response, map[string]any) {
			return annaEnv.do(t, "PUT", at(me), map[string]any{"role": "admin", "disabled": true}, ah)
		},
		"delete": func() (*http.Response, map[string]any) { return annaEnv.do(t, "DELETE", at(me), nil, ah) },
	} {
		if resp, body := do(); resp.StatusCode != 400 || body["code"] != "last_admin" {
			t.Errorf("%s the last administrator with a password, by one from the provider: %d %v", what, resp.StatusCode, body)
		}
	}
}
