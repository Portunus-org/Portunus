package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Location is where a server is, for the map.
type Location struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	// City and Country as reported by the lookup service; empty for a
	// place set by hand.
	City        string `json:"city,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"countryCode,omitempty"`
	// Manual: placed on the map by the operator; never overwritten.
	Manual bool `json:"manual,omitempty"`
}

func (l *Location) valid() bool {
	return l != nil && l.Lat >= -90 && l.Lat <= 90 && l.Lon >= -180 && l.Lon <= 180
}

const (
	settingServerLocation = "server_location"
	// locateRetry is how long to wait after a failed lookup.
	locateRetry = 2 * time.Minute
)

// locate asks the geolocation service where host is. Addresses that are
// not public have no place on a map and are not sent anywhere.
func (c *Core) locate(ctx context.Context, host string) (*Location, error) {
	if c.geoURL == "" {
		return nil, errors.New("geolocation lookups are off")
	}
	if host == "" {
		return nil, errors.New("no address")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addrs) == 0 {
			return nil, fmt.Errorf("resolve %s: %w", host, err)
		}
		ip = addrs[0].IP
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return nil, errors.New("not a public address")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if c.geoURL != GeoAuto {
		return geoAsk(ctx, c.geoURL+url.PathEscape(ip.String()))
	}
	// No free service places every address right, and each is wrong in its
	// own way (one put a Moscow address in Washington). So several are
	// asked at once and the country most of them name is taken; within it,
	// the answer of the first service that names that country. A city can
	// still be off: that is what placing a server by hand is for.
	answers := make([]*Location, len(geoServices))
	var wg sync.WaitGroup
	for i, service := range geoServices {
		wg.Go(func() { answers[i], _ = geoAsk(ctx, fmt.Sprintf(service, url.PathEscape(ip.String()))) })
	}
	wg.Wait()
	votes := map[string]int{}
	for _, a := range answers {
		if a != nil && a.CountryCode != "" {
			votes[a.CountryCode]++
		}
	}
	var best *Location
	for _, a := range answers {
		if a != nil && (best == nil || votes[a.CountryCode] > votes[best.CountryCode]) {
			best = a
		}
	}
	if best == nil {
		return nil, errors.New("no location for this address")
	}
	return best, nil
}

// GeoAuto asks the built-in services and takes what most of them say.
const GeoAuto = "auto"

// geoServices are address templates of services that answer without a key.
var geoServices = []string{
	"https://ipapi.co/%s/json/",
	"https://get.geojs.io/v1/ip/geo/%s.json",
	"https://ipwho.is/%s",
}

// geoAsk reads one service's answer. They agree on the names of the fields
// well enough; numbers come as numbers or as text.
func geoAsk(ctx context.Context, address string) (*Location, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var body struct {
		Success     *bool       `json:"success"`
		Error       bool        `json:"error"`
		Latitude    json.Number `json:"latitude"`
		Longitude   json.Number `json:"longitude"`
		City        string      `json:"city"`
		Country     string      `json:"country"`
		CountryName string      `json:"country_name"`
		CountryCode string      `json:"country_code"`
	}
	dec := json.NewDecoder(io.LimitReader(res.Body, 1<<20))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		return nil, err
	}
	lat, _ := strconv.ParseFloat(strings.Trim(body.Latitude.String(), `"`), 64)
	lon, _ := strconv.ParseFloat(strings.Trim(body.Longitude.String(), `"`), 64)
	if body.CountryName != "" {
		body.Country = body.CountryName
	}
	loc := &Location{Lat: lat, Lon: lon, City: body.City, Country: body.Country, CountryCode: strings.ToUpper(body.CountryCode)}
	if res.StatusCode != http.StatusOK || body.Error || (body.Success != nil && !*body.Success) || !loc.valid() || (loc.Lat == 0 && loc.Lon == 0) {
		return nil, errors.New("no location for this address")
	}
	return loc, nil
}

// ServerLocation is where the main server is, if known.
func (c *Core) ServerLocation(ctx context.Context) *Location {
	raw, _ := c.store.Setting(ctx, settingServerLocation)
	var loc Location
	if raw == "" || json.Unmarshal([]byte(raw), &loc) != nil || !loc.valid() {
		return nil
	}
	return &loc
}

// SetServerLocation places the main server on the map by hand; nil goes
// back to automatic lookup.
func (c *Core) SetServerLocation(ctx context.Context, loc *Location) error {
	if loc == nil {
		c.geoMu.Lock()
		delete(c.geoTried, 0)
		c.geoMu.Unlock()
		return c.store.SetSetting(ctx, settingServerLocation, "")
	}
	if !loc.valid() {
		return invalid("location", "out_of_range")
	}
	loc.Manual, loc.City, loc.Country, loc.CountryCode = true, "", "", ""
	raw, _ := json.Marshal(loc)
	return c.store.SetSetting(ctx, settingServerLocation, string(raw))
}

// LocateServers finds places for servers that have none yet. A failed
// lookup is not repeated for a while.
func (c *Core) LocateServers(ctx context.Context) {
	if c.geoURL == "" {
		return
	}
	due := func(id int64) bool {
		c.geoMu.Lock()
		defer c.geoMu.Unlock()
		if t, ok := c.geoTried[id]; ok && c.now().Sub(t) < locateRetry {
			return false
		}
		c.geoTried[id] = c.now()
		return true
	}
	if c.ServerLocation(ctx) == nil && due(0) {
		// The address clients use may be a domain or, behind NAT, not the
		// public one: prefer what detection says.
		host, _ := c.PublicHost(ctx)
		if addr, err := c.DetectAddress(ctx); err == nil && addr.Primary() != "" {
			host = addr.Primary()
		}
		loc, err := c.locate(ctx, host)
		switch {
		case err == nil:
			raw, _ := json.Marshal(loc)
			c.store.SetSetting(ctx, settingServerLocation, string(raw))
		case host == "":
			// Nothing to look up yet (first start): try again next round.
			c.geoMu.Lock()
			delete(c.geoTried, 0)
			c.geoMu.Unlock()
		default:
			c.log.Info("could not locate the main server for the map", "err", err)
		}
	}
	nodes, err := c.Nodes(ctx)
	if err != nil {
		return
	}
	for _, n := range nodes {
		if n.Settings.Location != nil || !due(n.ID) {
			continue
		}
		loc, err := c.locate(ctx, n.Address)
		if err != nil {
			c.log.Debug("locate node", "node", n.Name, "err", err)
			continue
		}
		// Re-read: the node may have changed while we were asking.
		fresh, err := c.Node(ctx, n.ID)
		if err != nil || fresh.Settings.Location != nil {
			continue
		}
		fresh.Settings.Location = loc
		if err := c.saveNode(ctx, fresh); err != nil {
			c.log.Error("save node location", "node", n.Name, "err", err)
		}
	}
}

// SetNodeLocation places a node on the map by hand; nil goes back to
// automatic lookup.
func (c *Core) SetNodeLocation(ctx context.Context, id int64, loc *Location) error {
	n, err := c.Node(ctx, id)
	if err != nil {
		return err
	}
	if loc != nil {
		if !loc.valid() {
			return invalid("location", "out_of_range")
		}
		loc = &Location{Lat: loc.Lat, Lon: loc.Lon, Manual: true}
	} else {
		c.geoMu.Lock()
		delete(c.geoTried, id)
		c.geoMu.Unlock()
	}
	n.Settings.Location = loc
	return c.saveNode(ctx, n)
}
