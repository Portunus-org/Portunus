// Package oidc signs people in through an OpenID Connect provider (Pocket
// ID, Authentik, Keycloak, any that follows the standard): the authorization
// code flow with PKCE, which is the one meant for a server like this.
//
// What each check is for:
//
//   - state ties the answer to the browser that asked, so that nobody can be
//     signed in to someone else's account by a link sent to them;
//   - the PKCE verifier ties the code to this very attempt, so that a code
//     seen on the way is of no use to whoever saw it;
//   - nonce ties the ID token to this attempt, so that a token issued for
//     another sign-in cannot be replayed;
//   - the ID token's signature, issuer, audience and expiry are checked by
//     go-oidc against the provider's published keys.
//
// A person is identified by the provider's subject and never by a name or
// an address, which the provider lets people change.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config is what the panel is told about its provider.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// GroupsClaim names the claim that lists a person's groups; a path with
	// dots reaches into an object ("realm_access.roles").
	GroupsClaim string
}

// Identity is who the provider says signed in.
type Identity struct {
	Subject string
	// Username is what to call the account: the provider's login name, or
	// the address when it gives none.
	Username string
	Name     string
	Groups   []string
}

// Flow is one sign-in attempt, kept in the person's browser (sealed) between
// leaving for the provider and coming back.
type Flow struct {
	State    string    `json:"s"`
	Nonce    string    `json:"n"`
	Verifier string    `json:"v"`
	Redirect string    `json:"r"`
	Started  time.Time `json:"t"`
}

// FlowLifetime is how long a person has to sign in at the provider.
const FlowLifetime = 10 * time.Minute

// Provider is a provider whose endpoints and keys are known.
type Provider struct {
	cfg      Config
	provider *gooidc.Provider
	client   *http.Client
}

// ValidIssuer checks the address of a provider. It has to be HTTPS: the
// sign-in rests on what is fetched from it. Plain HTTP is taken for this
// machine only (a provider run next to the panel, or a test).
func ValidIssuer(issuer string) error {
	u, err := url.Parse(issuer)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("not an address of a provider")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
			return nil
		}
		return errors.New("a provider must be reached over HTTPS")
	}
	return errors.New("not an address of a provider")
}

// Discover reads the provider's own description of itself (its endpoints
// and where its keys are) from the well-known address under the issuer.
func Discover(ctx context.Context, cfg Config) (*Provider, error) {
	if err := ValidIssuer(cfg.Issuer); err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		// A provider has no reason to send the panel elsewhere, and a
		// redirect is how a request ends up where it was not meant to go.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	ctx, cancel := context.WithTimeout(gooidc.ClientContext(ctx, client), 15*time.Second)
	defer cancel()
	p, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		// The same provider is often written with and without the slash.
		alt := strings.TrimSuffix(cfg.Issuer, "/")
		if alt == cfg.Issuer {
			alt += "/"
		}
		if p2, err2 := gooidc.NewProvider(ctx, alt); err2 == nil {
			p, err, cfg.Issuer = p2, nil, alt
		}
	}
	if err != nil {
		return nil, fmt.Errorf("the provider did not describe itself: %w", err)
	}
	return &Provider{cfg: cfg, provider: p, client: client}, nil
}

// Issuer is the provider's name for itself, as it stands in its tokens.
func (p *Provider) Issuer() string { return p.cfg.Issuer }

func (p *Provider) oauth(redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		Endpoint:     p.provider.Endpoint(),
		RedirectURL:  redirect,
		Scopes:       p.cfg.Scopes,
	}
}

func random() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Begin starts an attempt: where to send the browser, and what to remember
// until it comes back to redirect.
func (p *Provider) Begin(redirect string, now time.Time) (string, Flow) {
	f := Flow{State: random(), Nonce: random(), Verifier: oauth2.GenerateVerifier(), Redirect: redirect, Started: now}
	return p.oauth(redirect).AuthCodeURL(f.State, gooidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), f
}

// ErrFlow is an answer that does not belong to the attempt it claims.
var ErrFlow = errors.New("the sign-in was not started here, or took too long")

// Finish turns the provider's answer into an identity.
func (p *Provider) Finish(ctx context.Context, f Flow, state, code string, now time.Time) (Identity, error) {
	var id Identity
	if f.State == "" || code == "" || subtle.ConstantTimeCompare([]byte(f.State), []byte(state)) != 1 || now.Sub(f.Started) > FlowLifetime || now.Before(f.Started.Add(-time.Minute)) {
		return id, ErrFlow
	}
	ctx, cancel := context.WithTimeout(gooidc.ClientContext(ctx, p.client), 20*time.Second)
	defer cancel()
	token, err := p.oauth(f.Redirect).Exchange(ctx, code, oauth2.VerifierOption(f.Verifier))
	if err != nil {
		return id, fmt.Errorf("the provider did not accept the code: %w", err)
	}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return id, errors.New("the provider sent no ID token")
	}
	idToken, err := p.provider.Verifier(&gooidc.Config{ClientID: p.cfg.ClientID, Now: func() time.Time { return now }}).Verify(ctx, raw)
	if err != nil {
		return id, fmt.Errorf("the ID token does not check out: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(f.Nonce)) != 1 {
		return id, errors.New("the ID token belongs to another sign-in")
	}
	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return id, err
	}
	// Providers differ in what they put into the ID token and what they
	// keep for the userinfo endpoint. What the token lacks is asked for
	// there, and only under the same subject.
	if _, has := lookup(claims, p.cfg.GroupsClaim); !has || text(claims["preferred_username"]) == "" {
		if info, err := p.provider.UserInfo(ctx, oauth2.StaticTokenSource(token)); err == nil && info.Subject == idToken.Subject {
			more := map[string]any{}
			if info.Claims(&more) == nil {
				for k, v := range more {
					if _, set := claims[k]; !set {
						claims[k] = v
					}
				}
			}
		}
	}
	id.Subject = idToken.Subject
	if id.Subject == "" {
		return id, errors.New("the ID token names nobody")
	}
	id.Name = clip(text(claims["name"]), 128)
	id.Username = clip(first(text(claims["preferred_username"]), text(claims["email"]), id.Name), 64)
	if v, ok := lookup(claims, p.cfg.GroupsClaim); ok {
		id.Groups = list(v)
	}
	return id, nil
}

func first(vs ...string) string {
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// lookup follows a path with dots into nested claims.
func lookup(claims map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	if v, ok := claims[path]; ok {
		return v, true
	}
	var cur any = claims
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// list reads groups the ways providers write them: a list of names, or one
// name, or names separated by spaces or commas.
func list(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if s := strings.TrimSpace(text(e)); s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, s := range strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == ' ' }) {
			out = append(out, s)
		}
	}
	return out
}
