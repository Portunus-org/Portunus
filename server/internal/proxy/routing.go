package proxy

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// Route actions.
const (
	RouteDirect = "direct"
	RouteBlock  = "block"
	// RouteAuto sends traffic through the exit its client is bound to.
	RouteAuto = "auto"
	// RouteNode sends traffic through one particular exit.
	RouteNode = "node"
)

// Exit is a way out for traffic other than this server's own address: a hop
// to one of our nodes, or an imported third-party proxy. Outbound is the
// complete sing-box outbound except for its tag.
type Exit struct {
	ID       int64
	Outbound map[string]any
}

// Tag is the sing-box outbound tag of the exit with this node ID.
func ExitTag(nodeID int64) string { return fmt.Sprintf("node-%d", nodeID) }

// RouteList is a parsed set of destinations.
type RouteList struct {
	ID       int64
	Suffixes []string // a domain and its subdomains
	Domains  []string // exact names
	Keywords []string
	CIDRs    []string
	// Rules, when set, is a sing-box rule-set taken as it is (already
	// validated by sing-box); the fields above are then unused.
	Rules []any
}

func (l RouteList) Empty() bool {
	return len(l.Suffixes)+len(l.Domains)+len(l.Keywords)+len(l.CIDRs)+len(l.Rules) == 0
}

// ParseRuleSet reads the JSON array of rules of a sing-box rule-set.
func ParseRuleSet(id int64, rulesJSON string) (RouteList, error) {
	l := RouteList{ID: id}
	if err := json.Unmarshal([]byte(rulesJSON), &l.Rules); err != nil {
		return l, err
	}
	return l, nil
}

// Count says how many sites and how many addresses or networks the list
// names, for showing to the operator.
func (l RouteList) Count() (sites, addresses int) {
	if l.Rules == nil {
		return len(l.Suffixes) + len(l.Domains) + len(l.Keywords), len(l.CIDRs)
	}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case []any:
			for _, x := range t {
				walk(x)
			}
		case map[string]any:
			for k, x := range t {
				n := 1
				if list, ok := x.([]any); ok {
					n = len(list)
				}
				switch k {
				case "domain", "domain_suffix", "domain_keyword", "domain_regex":
					sites += n
				case "ip_cidr":
					addresses += n
				case "rules":
					walk(x)
				}
			}
		}
	}
	walk(l.Rules)
	return sites, addresses
}

// usesAddresses reports whether matching the list can need the resolved
// address of a destination.
func (l RouteList) usesAddresses() bool {
	_, addresses := l.Count()
	return addresses > 0
}

func (l RouteList) tag() string { return fmt.Sprintf("list-%d", l.ID) }

// MaxListEntries bounds a list; larger ones are cut off.
const MaxListEntries = 200_000

// ParseList reads a list of destinations, one per line:
//
//	example.com          the domain and its subdomains
//	full:example.com     that exact name only
//	keyword:video        any name containing the word
//	203.0.113.0/24       a network; 203.0.113.7 or 2001:db8::1 an address
//
// Blank lines and anything after # are ignored, as are lines it cannot
// read (returned as skipped), so a list from the internet with a few odd
// entries still works.
func ParseList(id int64, text string) (list RouteList, skipped int) {
	list.ID = id
	n := 0
	for line := range strings.SplitSeq(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" {
			continue
		}
		if n >= MaxListEntries {
			skipped++
			continue
		}
		switch {
		case strings.HasPrefix(line, "full:"):
			if d := strings.TrimPrefix(line, "full:"); validListDomain(d) {
				list.Domains = append(list.Domains, d)
				n++
				continue
			}
		case strings.HasPrefix(line, "keyword:"):
			if k := strings.TrimPrefix(line, "keyword:"); k != "" && !strings.ContainsAny(k, " \t/") {
				list.Keywords = append(list.Keywords, k)
				n++
				continue
			}
		default:
			if p, err := netip.ParsePrefix(line); err == nil {
				list.CIDRs = append(list.CIDRs, p.Masked().String())
				n++
				continue
			}
			if a, err := netip.ParseAddr(line); err == nil {
				list.CIDRs = append(list.CIDRs, netip.PrefixFrom(a, a.BitLen()).String())
				n++
				continue
			}
			d := strings.TrimPrefix(strings.TrimPrefix(line, "domain:"), "*.")
			// ".ua" style entries (a whole zone) keep their leading dot.
			if validListDomain(strings.TrimPrefix(d, ".")) {
				list.Suffixes = append(list.Suffixes, d)
				n++
				continue
			}
		}
		skipped++
	}
	return list, skipped
}

func validListDomain(d string) bool {
	if d == "" || len(d) > 253 {
		return false
	}
	for label := range strings.SplitSeq(d, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

// RouteRule sends what List matches to Action (and Exit, for RouteNode).
type RouteRule struct {
	List   RouteList
	Action string
	ExitID int64
}

// Binding ties clients to the exit their traffic leaves through under
// RouteAuto. One client keeps one exit, on every server it may enter
// through, so the address sites see for it stays the same.
type Binding struct {
	ExitID int64
	// Users are the clients' names inside sing-box (Client.UserName).
	Users []string
	// Local: this server is that exit, so the traffic simply leaves here.
	Local bool
}

// Routing is everything that decides where traffic leaves.
type Routing struct {
	// Exits that exist and are enabled; rules pointing elsewhere fail
	// closed (see target).
	Exits []Exit
	Rules []RouteRule
	// Default is the action for traffic no rule matches; DefaultExit goes
	// with RouteNode.
	Default     string
	DefaultExit int64
	// Bindings say which exit RouteAuto means for which client. A client
	// in none of them is refused there.
	Bindings []Binding
	// ControlAPI, when set, enables sing-box's local control API (used to
	// read exit latency); ControlSecret protects it.
	ControlAPI    string
	ControlSecret string
	// hop: this server also accepts traffic from the other servers (it is
	// an exit). Set by NodeConfig.
	hop bool
}

// rejectTag marks a destination that must not be served: an exit that no
// longer exists or is disabled. Traffic meant for it is refused rather than
// silently sent out directly.
const rejectTag = ""

// boundTag stands for "the exit of whoever is asking": not an outbound, it
// is spelt out per binding (see bound).
const boundTag = "\x00bound"

// target maps an action to an outbound tag; rejectTag means "refuse".
func (r Routing) target(action string, exitID int64) string {
	switch action {
	case RouteDirect, "":
		return "direct"
	case RouteAuto:
		return boundTag
	case RouteNode:
		for _, e := range r.Exits {
			if e.ID == exitID {
				return ExitTag(exitID)
			}
		}
	}
	return rejectTag
}

func routeAction(match obj, outbound string) obj {
	if outbound == rejectTag {
		match["action"] = "reject"
	} else {
		match["action"] = "route"
		match["outbound"] = outbound
	}
	return match
}

// apply writes outbounds, rule-sets and route rules into cfg.
//
// Order matters twice. First for the operator: rules are evaluated top to
// bottom. Second for privacy and safety: a domain headed for an exit should
// not be resolved here at all (this server's DNS may be the censored one),
// while anything leaving directly must be resolved so the private-address
// guard applies. So rules run in two passes around the resolve step:
// before it, rules that send traffic away (matched by name); after it,
// every rule again, now also matching by resolved address.
//
// sing-box replaces the destination with the resolved address, so what is
// routed to an exit in the second pass arrives there as an address, not a
// name. That only happens when a list of addresses has to be consulted;
// name-based rules and a plain "everything else through an exit" never
// resolve here.
func (r Routing) apply(cfg obj) {
	outbounds := []any{obj{"type": "direct", "tag": "direct"}}
	for _, e := range r.Exits {
		ob := obj{}
		for k, v := range e.Outbound {
			ob[k] = v
		}
		ob["tag"] = ExitTag(e.ID)
		outbounds = append(outbounds, ob)
	}
	cfg["outbounds"] = outbounds

	var (
		sets      []any
		rules     = []any{obj{"action": "sniff"}}
		direct    []string // lists already claimed by a "direct" rule
		claims    []any    // the same for clients whose own exit is this server
		needsAddr bool     // some rule can only match a resolved address
		active    []RouteRule
	)
	if r.hop {
		// What another server sent here was routed where it entered, by
		// that client's rules: it only has to leave. The rules below are
		// for clients that enter here, and the hop is not one of them.
		hop := func(o obj) obj {
			o["inbound"] = []string{hopTag}
			return o
		}
		rules = append(rules,
			hop(obj{"action": "resolve"}),
			hop(obj{"ip_is_private": true, "action": "reject"}),
			hop(obj{"action": "route", "outbound": "direct"}),
		)
	}
	for _, rule := range r.Rules {
		if rule.List.Empty() {
			continue
		}
		active = append(active, rule)
		needsAddr = needsAddr || rule.List.usesAddresses()
		content := rule.List.Rules
		if content == nil {
			set := obj{}
			if len(rule.List.Suffixes) > 0 {
				set["domain_suffix"] = rule.List.Suffixes
			}
			if len(rule.List.Domains) > 0 {
				set["domain"] = rule.List.Domains
			}
			if len(rule.List.Keywords) > 0 {
				set["domain_keyword"] = rule.List.Keywords
			}
			if len(rule.List.CIDRs) > 0 {
				set["ip_cidr"] = rule.List.CIDRs
			}
			content = []any{set}
		}
		tag := rule.List.tag()
		dup := false
		for _, s := range sets {
			dup = dup || s.(obj)["tag"] == tag
		}
		if !dup {
			sets = append(sets, obj{"type": "inline", "tag": tag, "rules": content})
		}
	}

	// notDirect narrows a match to what nothing earlier claimed for leaving
	// directly: that is decided after resolving, in the second pass.
	notDirect := func(match obj) obj {
		if len(direct)+len(claims) == 0 {
			return match
		}
		var not []any
		if len(direct) > 0 {
			not = append(not, obj{"rule_set": append([]string(nil), direct...), "invert": true})
		}
		not = append(not, claims...)
		return obj{"type": "logical", "mode": "and", "rules": append(not, match)}
	}
	// bound sends what match selects through each client's own exit and
	// refuses it for clients without one. Before resolving (early), clients
	// whose exit is this very server are left for the second pass: their
	// traffic leaves directly, so it has to be resolved and guarded first.
	bound := func(match func() obj, early bool) {
		var local []any
		for _, b := range r.Bindings {
			if len(b.Users) == 0 {
				continue
			}
			m := match()
			m["auth_user"] = b.Users
			switch {
			case !b.Local && early:
				rules = append(rules, routeAction(notDirect(m), r.target(RouteNode, b.ExitID)))
			case !b.Local:
				rules = append(rules, routeAction(m, r.target(RouteNode, b.ExitID)))
			case early:
				m["invert"] = true
				local = append(local, m)
			default:
				rules = append(rules, routeAction(m, "direct"))
			}
		}
		claims = append(claims, local...)
	}
	everything := func() obj { return obj{"network": []string{"tcp", "udp"}} }

	// Pass 1, before resolving: rules that send traffic away.
	for _, rule := range active {
		set := func() obj { return obj{"rule_set": []string{rule.List.tag()}} }
		switch out := r.target(rule.Action, rule.ExitID); out {
		case "direct":
			direct = append(direct, rule.List.tag())
		case boundTag:
			bound(set, true)
			rules = append(rules, routeAction(notDirect(set()), rejectTag))
		default:
			rules = append(rules, routeAction(notDirect(set()), out))
		}
	}

	final := r.target(r.Default, r.DefaultExit)
	if final != "direct" && !needsAddr {
		// Everything else leaves through an exit (or is refused) and no rule
		// needs an address: do not resolve it here.
		switch {
		case final == boundTag:
			bound(everything, true)
			rules = append(rules, routeAction(notDirect(everything()), rejectTag))
		case len(claims) > 0:
			rules = append(rules, routeAction(notDirect(everything()), final))
		case len(direct) > 0:
			rules = append(rules, routeAction(obj{"rule_set": append([]string(nil), direct...), "invert": true}, final))
		default:
			rules = append(rules, routeAction(everything(), final))
		}
		final = "direct"
	}

	// What is left may leave directly: resolve it, and never let clients
	// reach this host's loopback or private networks (the panel and the
	// stats API listen there).
	rules = append(rules,
		obj{"action": "resolve"},
		obj{"ip_is_private": true, "action": "reject"},
	)

	// Pass 2: every rule, now that addresses are known.
	for _, rule := range active {
		set := func() obj { return obj{"rule_set": []string{rule.List.tag()}} }
		out := r.target(rule.Action, rule.ExitID)
		if out == boundTag {
			bound(set, false)
			out = rejectTag
		}
		rules = append(rules, routeAction(set(), out))
	}
	if final == boundTag {
		bound(everything, false)
		final = rejectTag
	}
	if final == rejectTag {
		rules = append(rules, obj{"network": []string{"tcp", "udp"}, "action": "reject"})
		final = "direct"
	}

	route := obj{
		"rules":                   rules,
		"final":                   final,
		"default_domain_resolver": "local",
	}
	if len(sets) > 0 {
		route["rule_set"] = sets
	}
	cfg["route"] = route

	if r.ControlAPI != "" && len(r.Exits) > 0 {
		exp, _ := cfg["experimental"].(obj)
		if exp == nil {
			exp = obj{}
			cfg["experimental"] = exp
		}
		exp["clash_api"] = obj{"external_controller": r.ControlAPI, "secret": r.ControlSecret}
	}
}

// HopSettings and HopSecret describe the link from the panel's servers to
// an exit node of ours: a VLESS + REALITY inbound on the node with a single
// user. It looks like TLS to a real site, the same as a client connection.
type Hop struct {
	Port     int
	Settings Settings
	Secret   Secret
	UUID     string
}

// NewHop generates the keys of a hop on port.
func NewHop(port int, realityServer string) (Hop, error) {
	set, sec, err := Provision(VLESS, ProvisionOptions{RealityServer: realityServer})
	if err != nil {
		return Hop{}, err
	}
	return Hop{Port: port, Settings: set, Secret: sec, UUID: NewUUID()}, nil
}

const hopTag = "hop"

func (h Hop) inbound() Inbound {
	return Inbound{Tag: hopTag, Protocol: VLESS, Port: h.Port, Enabled: true, Settings: h.Settings, Secret: h.Secret}
}

func (h Hop) client() Client { return Client{ID: 0, Name: "hop", UUID: h.UUID} }

// Outbound is how another server reaches the exit at address.
func (h Hop) Outbound(address string) map[string]any {
	ob := ClientOutbound(address, h.inbound(), h.client())
	delete(ob, "tag")
	// UDP (QUIC, DNS, calls) through the TCP hop.
	ob["packet_encoding"] = "xudp"
	return ob
}

// NodeConfig renders the sing-box configuration of one of our nodes.
//
// hop, when set, makes it an exit: it accepts traffic from the other
// servers and sends it out. entry, when set, makes it a backup entry point:
// the given inbounds with the given clients, routed like on the main
// server. It returns nil when the node has nothing to run.
func NodeConfig(hop *Hop, entry *EntryConfig) ([]byte, error) {
	var (
		inbounds []Inbound
		clients  []Client
		routing  Routing
		statsAPI string
	)
	if entry != nil {
		inbounds, clients, routing, statsAPI = entry.Inbounds, entry.Clients, entry.Routing, entry.StatsAPI
	}
	return serverConfig(inbounds, clients, statsAPI, ACMEOptions{}, routing, hop)
}

// EntryConfig is what a backup entry point serves.
type EntryConfig struct {
	Inbounds []Inbound
	Clients  []Client
	Routing  Routing
	StatsAPI string
}
