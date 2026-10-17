package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/Portunus-org/Portunus/server/internal/singbox"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

// Route list kinds.
const (
	ListManual = "manual"
	ListURL    = "url"
)

const (
	settingRouteDefault = "route_default"
	// maxListBytes bounds a list, typed or downloaded.
	maxListBytes = 8 << 20
	// A typed list arrives in a request body, which is capped lower.
	maxManualListBytes = 512 << 10
	// listRefresh is how often downloaded lists are fetched again.
	listRefresh = 24 * time.Hour
	listRetry   = time.Hour
)

// RouteListInput is what the operator enters for a list.
type RouteListInput struct {
	Name    string
	Kind    string
	URL     string
	Content string
}

func validateRouteList(in *RouteListInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)
	if in.Name == "" {
		return invalid("name", "required")
	}
	if len(in.Name) > 64 {
		return invalid("name", "too_long")
	}
	if oddText(in.Name, false) {
		return invalid("name", "bad_characters")
	}
	switch in.Kind {
	case ListManual:
		in.URL = ""
		if len(in.Content) > maxManualListBytes {
			return invalid("content", "too_long")
		}
		if list, _ := proxy.ParseList(0, in.Content); list.Empty() {
			return invalid("content", "empty_list")
		}
	case ListURL:
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("url", "invalid_url")
		}
	default:
		return invalid("kind", "unsupported")
	}
	return nil
}

func (c *Core) RouteLists(ctx context.Context) ([]*store.RouteListRow, error) {
	return c.store.ListRouteLists(ctx)
}

// freeListName makes name usable for a list. To the operator a list is
// part of its rule and has no identity of its own, so a name is never
// "taken": a leftover list nobody uses gives way, and a second rule over
// the same set simply gets a number.
func (c *Core) freeListName(ctx context.Context, name string, self int64) (string, error) {
	lists, err := c.store.ListRouteLists(ctx)
	if err != nil {
		return "", err
	}
	rules, err := c.store.ListRouteRules(ctx)
	if err != nil {
		return "", err
	}
	used := map[int64]bool{}
	for _, r := range rules {
		used[r.ListID] = true
	}
	taken := map[string]bool{}
	for _, l := range lists {
		if l.ID == self {
			continue
		}
		if !used[l.ID] && strings.EqualFold(l.Name, name) {
			if err := c.store.DeleteRouteList(ctx, l.ID); err != nil {
				return "", err
			}
			continue
		}
		taken[strings.ToLower(l.Name)] = true
	}
	candidate := name
	for n := 2; taken[strings.ToLower(candidate)]; n++ {
		candidate = fmt.Sprintf("%s %d", name, n)
	}
	return candidate, nil
}

// dropUnusedLists removes lists no rule refers to: leftovers nobody can see.
func (c *Core) dropUnusedLists(ctx context.Context) {
	lists, err := c.store.ListRouteLists(ctx)
	if err != nil {
		return
	}
	rules, err := c.store.ListRouteRules(ctx)
	if err != nil {
		return
	}
	used := map[int64]bool{}
	for _, r := range rules {
		used[r.ListID] = true
	}
	for _, l := range lists {
		// A list just created for a rule being added is spared.
		if !used[l.ID] && c.now().Sub(l.UpdatedAt) > time.Hour {
			c.store.DeleteRouteList(ctx, l.ID)
		}
	}
}

func (c *Core) CreateRouteList(ctx context.Context, in RouteListInput) (*store.RouteListRow, error) {
	if err := validateRouteList(&in); err != nil {
		return nil, err
	}
	var err error
	if in.Name, err = c.freeListName(ctx, in.Name, 0); err != nil {
		return nil, err
	}
	row := &store.RouteListRow{Name: in.Name, Kind: in.Kind, URL: in.URL, Content: in.Content, Format: FormatText}
	if in.Kind == ListURL {
		// Download before saving: a list that cannot be fetched or holds
		// nothing usable is reported now, not found out later.
		text, format, err := c.fetchList(ctx, in.URL)
		if err != nil {
			return nil, err
		}
		row.Content, row.Format = text, format
	}
	if err := c.store.CreateRouteList(ctx, row); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, invalid("name", "taken")
		}
		return nil, err
	}
	if in.Kind == ListURL {
		c.store.SetRouteListFetch(ctx, row.ID, &row.Content, row.Format, "")
	}
	return c.store.RouteListByID(ctx, row.ID)
}

func (c *Core) UpdateRouteList(ctx context.Context, id int64, in RouteListInput) (*store.RouteListRow, error) {
	row, err := c.store.RouteListByID(ctx, id)
	if err != nil {
		return nil, err
	}
	in.Kind = row.Kind
	if err := validateRouteList(&in); err != nil {
		return nil, err
	}
	if in.Name, err = c.freeListName(ctx, in.Name, id); err != nil {
		return nil, err
	}
	row.Name = in.Name
	refetch := false
	if row.Kind == ListManual {
		row.Content = in.Content
	} else if row.URL != in.URL {
		text, format, err := c.fetchList(ctx, in.URL)
		if err != nil {
			return nil, err
		}
		row.URL, row.Content, row.Format, refetch = in.URL, text, format, true
	}
	if err := c.store.UpdateRouteList(ctx, row); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, invalid("name", "taken")
		}
		return nil, err
	}
	if refetch {
		c.store.SetRouteListFetch(ctx, id, &row.Content, row.Format, "")
	}
	c.Reconcile(ctx)
	return c.store.RouteListByID(ctx, id)
}

func (c *Core) DeleteRouteList(ctx context.Context, id int64) error {
	rules, err := c.store.ListRouteRules(ctx)
	if err != nil {
		return err
	}
	for _, r := range rules {
		if r.ListID == id {
			return invalid("list", "list_in_use")
		}
	}
	return c.store.DeleteRouteList(ctx, id)
}

// RefreshRouteList downloads a list again now.
func (c *Core) RefreshRouteList(ctx context.Context, id int64) (*store.RouteListRow, error) {
	row, err := c.store.RouteListByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Kind != ListURL {
		return row, nil
	}
	text, format, err := c.fetchList(ctx, row.URL)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			c.store.SetRouteListFetch(ctx, id, nil, "", ve.Code)
		}
		return nil, err
	}
	if err := c.store.SetRouteListFetch(ctx, id, &text, format, ""); err != nil {
		return nil, err
	}
	c.Reconcile(ctx)
	return c.store.RouteListByID(ctx, id)
}

// refreshRouteLists downloads lists that are due. The previous content
// stays in use when a download fails.
func (c *Core) refreshRouteLists(ctx context.Context) {
	lists, err := c.store.ListRouteLists(ctx)
	if err != nil {
		return
	}
	changed := false
	for _, l := range lists {
		// A failed download is retried sooner than a good list is renewed.
		every := listRefresh
		if l.FetchError != "" {
			every = listRetry
		}
		if l.Kind != ListURL || (l.FetchedAt != nil && c.now().Sub(*l.FetchedAt) < every) {
			continue
		}
		text, format, err := c.fetchList(ctx, l.URL)
		if err != nil {
			c.log.Warn("refresh list", "list", l.Name, "err", err)
			code := "fetch_failed"
			var ve *ValidationError
			if errors.As(err, &ve) {
				code = ve.Code
			}
			c.store.SetRouteListFetch(ctx, l.ID, nil, "", code)
			continue
		}
		if text != l.Content {
			changed = true
		}
		c.store.SetRouteListFetch(ctx, l.ID, &text, format, "")
	}
	if changed {
		c.Reconcile(ctx)
	}
}

// listHTTP downloads lists. It refuses to connect to loopback and private
// addresses, whatever the URL resolves or redirects to: a list URL is typed
// by an operator and must not become a way to read services that only
// listen locally (the panel itself, the statistics API).
var listHTTP = &http.Client{
	Timeout: 45 * time.Second,
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(network, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				ip := net.ParseIP(host)
				if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
					return errors.New("address not allowed")
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// Route list formats.
const (
	FormatText    = "text"
	FormatRuleSet = "ruleset"
)

// parseRouteList reads stored list content in its format.
func parseRouteList(id int64, content, format string) (list proxy.RouteList, skipped int) {
	if format == FormatRuleSet {
		list, _ = proxy.ParseRuleSet(id, content)
		return list, 0
	}
	return proxy.ParseList(id, content)
}

// fetchList downloads a list and returns its content and format. Three
// kinds of file are understood: a plain text list, and sing-box rule-sets
// in binary (.srs) or source (JSON) form, which sing-box itself converts
// and validates. Errors are validation errors on the "url" field so the
// form can show them.
func (c *Core) fetchList(ctx context.Context, rawURL string) (content, format string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", invalid("url", "invalid_url")
	}
	req.Header.Set("User-Agent", "Portunus")
	res, err := c.listClient().Do(req)
	if err != nil {
		c.log.Warn("download list", "url", rawURL, "err", err)
		return "", "", invalid("url", "fetch_failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", invalid("url", "fetch_failed")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxListBytes+1))
	if err != nil {
		return "", "", invalid("url", "fetch_failed")
	}
	if len(raw) > maxListBytes {
		return "", "", invalid("url", "too_long")
	}
	trimmed := bytes.TrimSpace(raw)
	if singbox.IsBinaryRuleSet(raw) || (len(trimmed) > 0 && trimmed[0] == '{') {
		rules, err := c.mgr.RuleSetRules(ctx, raw)
		if err != nil {
			c.log.Warn("read rule-set", "url", rawURL, "err", err)
			return "", "", invalid("url", "not_a_list")
		}
		return string(rules), FormatRuleSet, nil
	}
	if !utf8.Valid(raw) {
		return "", "", invalid("url", "not_a_list")
	}
	text := string(raw)
	list, skipped := proxy.ParseList(0, text)
	n := len(list.Suffixes) + len(list.Domains) + len(list.Keywords) + len(list.CIDRs)
	// An HTML page or another format parses as almost nothing.
	if n == 0 || skipped > n {
		return "", "", invalid("url", "not_a_list")
	}
	return text, FormatText, nil
}

func (c *Core) listClient() *http.Client {
	if c.listHTTP != nil {
		return c.listHTTP
	}
	return listHTTP
}

// ------------------------------------------------------------------ rules

// RouteRuleInput is one rule as the operator sets it.
type RouteRuleInput struct {
	ListID  int64
	Action  string
	NodeID  *int64
	Enabled bool
}

func (c *Core) validateRouteTarget(ctx context.Context, action string, nodeID *int64) error {
	switch action {
	case proxy.RouteDirect, proxy.RouteBlock, proxy.RouteAuto:
		return nil
	case proxy.RouteNode:
		if nodeID == nil {
			return invalid("node", "required")
		}
		n, err := c.store.NodeByID(ctx, *nodeID)
		if err != nil || !n.IsExit {
			return invalid("node", "not_an_exit")
		}
		return nil
	}
	return invalid("action", "unsupported")
}

func (c *Core) RouteRules(ctx context.Context) ([]*store.RouteRuleRow, error) {
	return c.store.ListRouteRules(ctx)
}

func (c *Core) CreateRouteRule(ctx context.Context, in RouteRuleInput) (*store.RouteRuleRow, error) {
	if _, err := c.store.RouteListByID(ctx, in.ListID); err != nil {
		return nil, invalid("list", "required")
	}
	if err := c.validateRouteTarget(ctx, in.Action, in.NodeID); err != nil {
		return nil, err
	}
	if in.Action != proxy.RouteNode {
		in.NodeID = nil
	}
	row := &store.RouteRuleRow{ListID: in.ListID, Action: in.Action, NodeID: in.NodeID, Enabled: in.Enabled}
	if err := c.store.CreateRouteRule(ctx, row); err != nil {
		return nil, err
	}
	c.Reconcile(ctx)
	return row, nil
}

func (c *Core) UpdateRouteRule(ctx context.Context, id int64, in RouteRuleInput) error {
	if _, err := c.store.RouteListByID(ctx, in.ListID); err != nil {
		return invalid("list", "required")
	}
	if err := c.validateRouteTarget(ctx, in.Action, in.NodeID); err != nil {
		return err
	}
	if in.Action != proxy.RouteNode {
		in.NodeID = nil
	}
	if err := c.store.UpdateRouteRule(ctx, &store.RouteRuleRow{ID: id, ListID: in.ListID, Action: in.Action, NodeID: in.NodeID, Enabled: in.Enabled}); err != nil {
		return err
	}
	c.Reconcile(ctx)
	return nil
}

// DeleteRouteRule removes a rule and, with it, a list no other rule uses:
// in the panel a rule and its list are one thing.
func (c *Core) DeleteRouteRule(ctx context.Context, id int64) error {
	rules, err := c.store.ListRouteRules(ctx)
	if err != nil {
		return err
	}
	var listID int64
	users := 0
	for _, r := range rules {
		if r.ID == id {
			listID = r.ListID
		}
	}
	for _, r := range rules {
		if r.ListID == listID {
			users++
		}
	}
	if err := c.store.DeleteRouteRule(ctx, id); err != nil {
		return err
	}
	if listID != 0 && users == 1 {
		if err := c.store.DeleteRouteList(ctx, listID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	c.Reconcile(ctx)
	return nil
}

// RouteListCounts says what a stored list holds.
func RouteListCounts(l *store.RouteListRow) (sites, addresses, skipped int) {
	list, skipped := parseRouteList(l.ID, l.Content, l.Format)
	sites, addresses = list.Count()
	return sites, addresses, skipped
}

// OrderRouteRules sets the evaluation order; ids must name every rule once.
func (c *Core) OrderRouteRules(ctx context.Context, ids []int64) error {
	rules, err := c.store.ListRouteRules(ctx)
	if err != nil {
		return err
	}
	have := map[int64]bool{}
	for _, r := range rules {
		have[r.ID] = true
	}
	if len(ids) != len(rules) {
		return invalid("order", "incomplete")
	}
	for _, id := range ids {
		if !have[id] {
			return invalid("order", "incomplete")
		}
		delete(have, id)
	}
	if err := c.store.OrderRouteRules(ctx, ids); err != nil {
		return err
	}
	c.Reconcile(ctx)
	return nil
}

// DefaultRoute is where traffic goes that no rule matches.
func (c *Core) DefaultRoute(ctx context.Context) (action string, nodeID int64) {
	v, _ := c.store.Setting(ctx, settingRouteDefault)
	if id, ok := strings.CutPrefix(v, "node:"); ok {
		if n, err := strconv.ParseInt(id, 10, 64); err == nil {
			return proxy.RouteNode, n
		}
	}
	if v == proxy.RouteAuto {
		return proxy.RouteAuto, 0
	}
	return proxy.RouteDirect, 0
}

func (c *Core) SetDefaultRoute(ctx context.Context, action string, nodeID *int64) error {
	if action == proxy.RouteBlock {
		return invalid("action", "unsupported")
	}
	if err := c.validateRouteTarget(ctx, action, nodeID); err != nil {
		return err
	}
	v := action
	if action == proxy.RouteNode {
		v = fmt.Sprintf("node:%d", *nodeID)
	}
	if err := c.store.SetSetting(ctx, settingRouteDefault, v); err != nil {
		return err
	}
	c.Reconcile(ctx)
	return nil
}

// routing assembles everything that decides where traffic leaves, as seen
// from one server: self is 0 for the main server, or the ID of the node the
// configuration is for. A server never routes through itself: what would
// go to its own exit simply leaves directly.
func (c *Core) routing(ctx context.Context, self int64) (proxy.Routing, error) {
	var r proxy.Routing
	nodes, err := c.Nodes(ctx)
	if err != nil {
		return r, err
	}
	var exits []int64
	for _, n := range nodes {
		if n.IsExit && n.Enabled {
			exits = append(exits, n.ID)
			if n.ID != self {
				r.Exits = append(r.Exits, n.exit())
			}
		}
	}
	if r.Bindings, err = c.bindings(ctx, self, exits); err != nil {
		return r, err
	}
	lists, err := c.store.ListRouteLists(ctx)
	if err != nil {
		return r, err
	}
	parsed := map[int64]proxy.RouteList{}
	for _, l := range lists {
		parsed[l.ID], _ = parseRouteList(l.ID, l.Content, l.Format)
	}
	rules, err := c.store.ListRouteRules(ctx)
	if err != nil {
		return r, err
	}
	local := func(action string, node int64) (string, int64) {
		if action == proxy.RouteNode && node == self {
			return proxy.RouteDirect, 0
		}
		return action, node
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		var node int64
		if rule.NodeID != nil {
			node = *rule.NodeID
		}
		action, node := local(rule.Action, node)
		r.Rules = append(r.Rules, proxy.RouteRule{List: parsed[rule.ListID], Action: action, ExitID: node})
	}
	r.Default, r.DefaultExit = local(c.DefaultRoute(ctx))
	if self == 0 {
		r.ControlAPI, r.ControlSecret = c.controlAPI, c.controlSecret()
	}
	return r, nil
}
