package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/PaulichP/Portunus/server/internal/oidc"
	"github.com/PaulichP/Portunus/server/internal/store"
)

// SSO is how people sign in through an OIDC provider, and what their
// groups there let them do here.
type SSO struct {
	Enabled bool `json:"enabled"`
	// Name is what the button on the sign-in page says ("Pocket ID").
	Name     string `json:"name"`
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`
	// HasSecret tells that a client secret is stored; the secret itself is
	// never shown again.
	HasSecret bool   `json:"hasSecret"`
	Scopes    string `json:"scopes"`
	// GroupsClaim names the claim with the person's groups.
	GroupsClaim string `json:"groupsClaim"`
	// The group that gives each role; empty: no group gives it.
	AdminGroup    string `json:"adminGroup"`
	OperatorGroup string `json:"operatorGroup"`
	ViewerGroup   string `json:"viewerGroup"`
	// DefaultRole is given to someone whose groups give none; empty: such a
	// person is not let in.
	DefaultRole string `json:"defaultRole"`
}

const (
	settingSSO       = "sso"
	settingSSOSecret = "sso_client_secret" // encrypted

	defaultScopes = "openid profile email groups"
)

type ssoRuntime struct {
	mu       sync.Mutex
	provider *oidc.Provider
	// key is what the provider was looked up with: any change of it means
	// looking again.
	key string
}

// SSO returns the settings, without the secret.
func (c *Core) SSO(ctx context.Context) (SSO, error) {
	s := SSO{Scopes: defaultScopes, GroupsClaim: "groups"}
	raw, err := c.store.Setting(ctx, settingSSO)
	if err != nil {
		return s, err
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return s, err
		}
	}
	sealed, _ := c.store.Setting(ctx, settingSSOSecret)
	s.HasSecret = sealed != ""
	return s, nil
}

func validRole(r string) bool {
	return r == string(store.RoleAdmin) || r == string(store.RoleOperator) || r == string(store.RoleViewer)
}

// SetSSO saves the settings. secret nil keeps the stored secret; a pointer
// to "" removes it (a public client, which has none).
func (c *Core) SetSSO(ctx context.Context, s SSO, secret *string) error {
	for _, f := range []*string{&s.Name, &s.Issuer, &s.ClientID, &s.Scopes, &s.GroupsClaim, &s.AdminGroup, &s.OperatorGroup, &s.ViewerGroup} {
		*f = strings.TrimSpace(*f)
	}
	if s.Scopes == "" {
		s.Scopes = defaultScopes
	}
	if s.GroupsClaim == "" {
		s.GroupsClaim = "groups"
	}
	// What is typed here is shown on pages, written to the log and compared
	// with what a provider sends: one line of ordinary text, of a length a
	// real value has.
	for field, v := range map[string]struct {
		text string
		max  int
	}{
		"name": {s.Name, 40}, "issuer": {s.Issuer, 512}, "clientId": {s.ClientID, 255}, "scopes": {s.Scopes, 512}, "groupsClaim": {s.GroupsClaim, 128},
		"adminGroup": {s.AdminGroup, 255}, "operatorGroup": {s.OperatorGroup, 255}, "viewerGroup": {s.ViewerGroup, 255},
	} {
		if len(v.text) > v.max {
			return invalid(field, "too_long")
		}
		if oddText(v.text, false) {
			return invalid(field, "bad_characters")
		}
	}
	if secret != nil && oddText(*secret, false) {
		return invalid("clientSecret", "bad_characters")
	}
	switch {
	case len(s.Name) > 40:
		return invalid("name", "too_long")
	case s.DefaultRole != "" && !validRole(s.DefaultRole):
		return invalid("defaultRole", "unsupported")
	case !slices.Contains(strings.Fields(s.Scopes), "openid"):
		return invalid("scopes", "openid_required")
	}
	if s.Enabled || s.Issuer != "" {
		if oidc.ValidIssuer(s.Issuer) != nil {
			return invalid("issuer", "not_a_provider")
		}
	}
	if s.Enabled {
		switch {
		case s.ClientID == "":
			return invalid("clientId", "required")
		// With no group and no default nobody could ever get in: a setting
		// that can only be a mistake.
		case s.AdminGroup == "" && s.OperatorGroup == "" && s.ViewerGroup == "" && s.DefaultRole == "":
			return invalid("defaultRole", "nobody_gets_in")
		}
	}
	s.HasSecret = false
	raw, _ := json.Marshal(s)
	if err := c.store.SetSetting(ctx, settingSSO, string(raw)); err != nil {
		return err
	}
	if secret != nil {
		sealed := ""
		if *secret != "" {
			sealed = c.box.Seal(*secret, "setting:"+settingSSOSecret)
		}
		if err := c.store.SetSetting(ctx, settingSSOSecret, sealed); err != nil {
			return err
		}
	}
	c.sso.mu.Lock()
	c.sso.provider, c.sso.key = nil, ""
	c.sso.mu.Unlock()
	return nil
}

func (c *Core) ssoConfig(ctx context.Context, s SSO) (oidc.Config, error) {
	cfg := oidc.Config{Issuer: s.Issuer, ClientID: s.ClientID, Scopes: strings.Fields(s.Scopes), GroupsClaim: s.GroupsClaim}
	sealed, err := c.store.Setting(ctx, settingSSOSecret)
	if err != nil {
		return cfg, err
	}
	if sealed != "" {
		if cfg.ClientSecret, err = c.box.Open(sealed, "setting:"+settingSSOSecret); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// ErrSSOOff is returned when sign-in through a provider is not turned on.
var ErrSSOOff = errors.New("sign-in through a provider is off")

// SSOProvider is the provider people sign in through. Its description is
// read once and kept until the settings change.
func (c *Core) SSOProvider(ctx context.Context) (*oidc.Provider, SSO, error) {
	s, err := c.SSO(ctx)
	if err != nil {
		return nil, s, err
	}
	if !s.Enabled {
		return nil, s, ErrSSOOff
	}
	cfg, err := c.ssoConfig(ctx, s)
	if err != nil {
		return nil, s, err
	}
	key := strings.Join([]string{cfg.Issuer, cfg.ClientID, cfg.ClientSecret, s.Scopes, s.GroupsClaim}, "\x00")
	c.sso.mu.Lock()
	defer c.sso.mu.Unlock()
	if c.sso.provider != nil && c.sso.key == key {
		return c.sso.provider, s, nil
	}
	p, err := oidc.Discover(ctx, cfg)
	if err != nil {
		return nil, s, err
	}
	c.sso.provider, c.sso.key = p, key
	return p, s, nil
}

// TestSSO asks the provider at issuer to describe itself, without saving
// anything: what the "check" button in the settings does.
func (c *Core) TestSSO(ctx context.Context, issuer string) error {
	_, err := oidc.Discover(ctx, oidc.Config{Issuer: strings.TrimSpace(issuer)})
	return err
}

// Role is what a person with these groups may do here: the highest role
// one of their groups gives, else the default; false when they are not to
// be let in. Group names are compared exactly: a group is a grant of
// access, not a search.
func (s SSO) Role(groups []string) (store.Role, bool) {
	for _, grant := range []struct {
		group string
		role  store.Role
	}{{s.AdminGroup, store.RoleAdmin}, {s.OperatorGroup, store.RoleOperator}, {s.ViewerGroup, store.RoleViewer}} {
		if grant.group != "" && slices.Contains(groups, grant.group) {
			return grant.role, true
		}
	}
	if validRole(s.DefaultRole) {
		return store.Role(s.DefaultRole), true
	}
	return "", false
}

// SealFlow and OpenFlow keep a sign-in attempt in the person's browser so
// that nobody, the person included, can read or change it.
func (c *Core) SealFlow(f oidc.Flow) string {
	raw, _ := json.Marshal(f)
	return c.box.Seal(string(raw), "sso-flow")
}

func (c *Core) OpenFlow(sealed string) (oidc.Flow, bool) {
	var f oidc.Flow
	raw, err := c.box.Open(sealed, "sso-flow")
	if err != nil || json.Unmarshal([]byte(raw), &f) != nil {
		return f, false
	}
	return f, true
}
