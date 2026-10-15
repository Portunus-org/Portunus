package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/agent"
	"github.com/Portunus-org/Portunus/server/internal/selfupdate"
	"github.com/Portunus-org/Portunus/server/internal/version"
)

// Update is what the panel knows about releases newer than itself.
type Update struct {
	// Current is the version of this build ("dev" for one that is not a
	// release).
	Current string `json:"current"`
	// Latest is the newest release published; "" until the first answer.
	Latest string `json:"latest,omitempty"`
	// Available: a release newer than this one exists.
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	// Error is why the last check failed; Latest is then from the check
	// before it.
	Error string `json:"error,omitempty"`
	// Off: the check is turned off (PORTUNUS_UPDATE_CHECK=off).
	Off bool `json:"off,omitempty"`

	// Auto: releases are installed by themselves, at Minute (of the day,
	// UTC) on Weekday (0 is Sunday, in UTC; -1: any day). Otherwise a
	// release waits for the button, or for the moment it is planned for.
	Auto    bool `json:"auto"`
	Weekday int  `json:"weekday"`
	Minute  int  `json:"minute"`
	// Planned is the moment the release that waits will be installed at,
	// when one was chosen for it.
	Planned *time.Time `json:"planned,omitempty"`
	// Updater: the container that installs updates is next to the panel
	// (internal/selfupdate); without it only the command line can.
	Updater bool `json:"updater"`
	// Installing is the release being installed right now.
	Installing string `json:"installing,omitempty"`
	// InstallError is why the last attempt to install Latest failed.
	InstallError string `json:"installError,omitempty"`
}

const (
	settingUpdateAuto    = "update_auto"
	settingUpdateWeekday = "update_weekday"
	settingUpdateMinute  = "update_minute"
	settingUpdatePlanned = "update_planned"
)

// UpdateSchedule is when releases are installed by themselves.
type UpdateSchedule struct {
	Auto    bool
	Weekday int // 0 is Sunday, UTC; -1: any day
	Minute  int // of the day, UTC
}

// updateSchedule is how updates are installed: by the button unless chosen
// otherwise, and then any day at 03:00 UTC unless chosen otherwise.
func (c *Core) updateSchedule(ctx context.Context) UpdateSchedule {
	s := UpdateSchedule{Weekday: -1, Minute: 3 * 60}
	if v, _ := c.store.Setting(ctx, settingUpdateAuto); v == "1" {
		s.Auto = true
	}
	if v, _ := c.store.Setting(ctx, settingUpdateWeekday); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= -1 && n <= 6 {
			s.Weekday = n
		}
	}
	if v, _ := c.store.Setting(ctx, settingUpdateMinute); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < 24*60 {
			s.Minute = n
		}
	}
	return s
}

func (c *Core) SetUpdateSchedule(ctx context.Context, s UpdateSchedule) error {
	if s.Weekday < -1 || s.Weekday > 6 {
		return invalid("weekday", "out_of_range")
	}
	if s.Minute < 0 || s.Minute >= 24*60 {
		return invalid("minute", "out_of_range")
	}
	auto := "0"
	if s.Auto {
		auto = "1"
	}
	for k, v := range map[string]string{settingUpdateAuto: auto, settingUpdateWeekday: strconv.Itoa(s.Weekday), settingUpdateMinute: strconv.Itoa(s.Minute)} {
		if err := c.store.SetSetting(ctx, k, v); err != nil {
			return err
		}
	}
	return nil
}

// planned is the moment chosen for the release that waits, if any.
func (c *Core) planned(ctx context.Context) *time.Time {
	v, _ := c.store.Setting(ctx, settingUpdatePlanned)
	at, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil
	}
	return &at
}

// PlanUpdate sets the moment the release that waits is installed at; nil
// takes the plan back. Only a moment ahead can be chosen, and there must be
// something to install and an updater to do it.
func (c *Core) PlanUpdate(ctx context.Context, at *time.Time) error {
	if at == nil {
		return c.store.SetSetting(ctx, settingUpdatePlanned, "")
	}
	st := c.UpdateState(ctx)
	switch {
	case !st.Updater:
		return invalid("update", "no_updater")
	case !st.Available:
		return invalid("update", "nothing_to_install")
	case !at.After(c.now()):
		return invalid("at", "in_the_past")
	case at.After(c.now().Add(90 * 24 * time.Hour)):
		return invalid("at", "too_far")
	}
	return c.store.SetSetting(ctx, settingUpdatePlanned, at.UTC().Format(time.RFC3339))
}

// InstallUpdate asks the updater next to the panel to put the newest
// release in the panel's place. The panel goes down for the moment that
// takes; the nodes follow by themselves once it is back (followPanel).
func (c *Core) InstallUpdate(ctx context.Context) error {
	st := c.UpdateState(ctx)
	switch {
	case !st.Updater:
		return invalid("update", "no_updater")
	case !st.Available:
		return invalid("update", "nothing_to_install")
	}
	// The new version may change the database, and the previous one cannot
	// read it then. Should the update be undone, this copy is what the
	// previous version comes back to (db.PutBack).
	if c.updates.before != "" {
		os.Remove(c.updates.before)
		if err := c.store.Snapshot(ctx, c.updates.before); err != nil {
			return fmt.Errorf("copy the database before the update: %w", err)
		}
		os.Chmod(c.updates.before, 0o600)
	}
	if err := selfupdate.Ask(c.updates.dir, st.Latest, time.Now()); err != nil {
		return err
	}
	// Whatever moment was chosen for it, it is being installed now.
	c.store.SetSetting(ctx, settingUpdatePlanned, "")
	c.log.Info("update asked for", "version", st.Latest)
	return nil
}

// followPanel asks a node to move to the panel's own release when it runs
// an older one: nodes are never ahead of the panel and never left behind.
// It reports whether the node is being updated.
func (c *Core) followPanel(ctx context.Context, n *Node, cl *agent.Client, st agent.Status) (updating bool) {
	if st.Updater == nil || !selfupdate.Newer(version.Version, st.Version) {
		return false
	}
	// A release that failed on this node is not asked for again and again.
	if st.Updater.State == selfupdate.StateFailed && st.Updater.Version == version.Version {
		return false
	}
	if st.Updater.State == selfupdate.StateUpdating {
		return true
	}
	u := &c.updates
	u.mu.Lock()
	recently := time.Since(u.nodeAsked[n.ID]) < 2*time.Minute
	if !recently {
		if u.nodeAsked == nil {
			u.nodeAsked = map[int64]time.Time{}
		}
		u.nodeAsked[n.ID] = time.Now()
	}
	u.mu.Unlock()
	if recently {
		return true
	}
	if _, err := cl.Update(ctx, version.Version); err != nil {
		c.log.Warn("ask a node to update", "node", n.Name, "err", err)
		return false
	}
	c.log.Info("node asked to update", "node", n.Name, "from", st.Version, "to", version.Version)
	return true
}

type updates struct {
	// source is the image repository releases are published to
	// ("ghcr.io/owner/name"); "" turns the check off.
	source string
	client *http.Client
	// checking lets one check run at a time: a second one, asked for while
	// the first is on its way, waits and takes its answer.
	checking sync.Mutex
	mu       sync.Mutex
	// dir is shared with the updater next to the panel; before is where
	// the database is copied to before an update.
	dir, before string
	// nodeAsked is when each node was last asked to update; scheduled is
	// the release the schedule already asked for.
	nodeAsked map[int64]time.Time
	scheduled string
	state     Update
	// asked is when the last check was finished.
	asked time.Time
}

const (
	updateEvery = 6 * time.Hour
	// A check asked for by hand is answered from memory when the last one
	// is this fresh: the button cannot be made to hammer the registry.
	updateFresh = 30 * time.Second
)

// UpdateState is what the last check found, without making one, and where
// installing it stands.
func (c *Core) UpdateState(ctx context.Context) Update {
	c.updates.mu.Lock()
	st := c.updates.state
	c.updates.mu.Unlock()
	st.Current, st.Off = version.Version, c.updates.source == ""
	sched := c.updateSchedule(ctx)
	st.Auto, st.Weekday, st.Minute = sched.Auto, sched.Weekday, sched.Minute
	if st.Available {
		st.Planned = c.planned(ctx)
	}
	if c.updates.dir != "" {
		up, there := selfupdate.Look(c.updates.dir, time.Now())
		st.Updater = there
		// Only what concerns a release ahead of this one: a note about the
		// release that now runs is history.
		if there && selfupdate.Newer(up.Version, version.Version) {
			switch up.State {
			case selfupdate.StateUpdating:
				st.Installing = up.Version
			case selfupdate.StateFailed:
				st.InstallError = up.Error
			}
		}
		// Asked for, and the updater has not got to it yet.
		if req, ok := selfupdate.ReadRequest(c.updates.dir); ok && there && st.InstallError == "" && selfupdate.Newer(req.Version, version.Version) && time.Since(req.At) < 10*time.Minute {
			st.Installing = req.Version
		}
	}
	return st
}

// CheckUpdate asks the registry which releases exist and remembers the
// answer.
func (c *Core) CheckUpdate(ctx context.Context) Update {
	u := &c.updates
	if u.source == "" {
		return c.UpdateState(ctx)
	}
	u.checking.Lock()
	defer u.checking.Unlock()
	u.mu.Lock()
	fresh := !u.asked.IsZero() && time.Since(u.asked) < updateFresh
	u.mu.Unlock()
	if fresh {
		return c.UpdateState(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	latest, err := latestRelease(ctx, u.client, u.source)
	u.mu.Lock()
	u.asked = time.Now()
	u.state.CheckedAt = c.now()
	if err != nil {
		u.state.Error = err.Error()
		c.log.Warn("check for a new version", "err", err)
	} else {
		u.state.Error, u.state.Latest = "", latest
		u.state.Available = selfupdate.Newer(latest, version.Version)
	}
	u.mu.Unlock()
	return c.UpdateState(ctx)
}

// RunUpdates looks for a new release shortly after the start and then a
// few times a day, until ctx ends.
func (c *Core) RunUpdates(ctx context.Context) {
	if c.updates.source == "" {
		return
	}
	wait, checked := 20*time.Second, time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = time.Minute
		if time.Since(checked) >= updateEvery {
			c.CheckUpdate(ctx)
			checked = time.Now()
		}
		c.installOnSchedule(ctx, time.Now())
	}
}

// due reports whether now falls into the hour that starts at the
// schedule's time of day, on its day of the week.
func (s UpdateSchedule) due(now time.Time) bool {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), s.Minute/60, s.Minute%60, 0, 0, time.UTC)
	if now.Before(start) {
		start = start.AddDate(0, 0, -1)
	}
	return now.Sub(start) < time.Hour && (s.Weekday < 0 || int(start.Weekday()) == s.Weekday)
}

// installOnSchedule installs a release that waits when its time has come:
// the moment planned for it, or the hour of the schedule when updates
// install themselves. A release is asked for once: one that failed is not
// tried again every minute of that hour, or the next night.
func (c *Core) installOnSchedule(ctx context.Context, now time.Time) {
	st := c.UpdateState(ctx)
	if !st.Available || !st.Updater {
		return
	}
	planned := st.Planned != nil && !now.Before(*st.Planned)
	if !planned && !(st.Auto && UpdateSchedule{Auto: true, Weekday: st.Weekday, Minute: st.Minute}.due(now)) {
		return
	}
	if planned {
		// The moment has come, whatever happens next: it is not kept to
		// fire again after a failure or a restart.
		c.store.SetSetting(ctx, settingUpdatePlanned, "")
	}
	u := &c.updates
	u.mu.Lock()
	again := u.scheduled == st.Latest
	u.scheduled = st.Latest
	u.mu.Unlock()
	if (again && !planned) || st.Installing != "" || (st.InstallError != "" && !planned) {
		return
	}
	if err := c.InstallUpdate(ctx); err != nil {
		c.log.Warn("scheduled update", "err", err)
	}
}

// latestRelease returns the newest release among the tags of an image
// repository, read the way any container client reads them: the registry's
// tag list, with the anonymous token the registry asks for.
func latestRelease(ctx context.Context, client *http.Client, source string) (string, error) {
	base := source
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || strings.Trim(u.Path, "/") == "" {
		return "", errors.New("the source of releases is not an image repository")
	}
	next := u.Scheme + "://" + u.Host + "/v2/" + strings.Trim(u.Path, "/") + "/tags/list?n=1000"
	token, found := "", ""
	for page := 0; next != "" && page < 10; page++ {
		res, err := registryGet(ctx, client, next, token)
		if err != nil {
			return "", err
		}
		if res.StatusCode == http.StatusUnauthorized && token == "" {
			challenge := res.Header.Get("WWW-Authenticate")
			res.Body.Close()
			if token, err = registryToken(ctx, client, challenge); err != nil {
				return "", err
			}
			page--
			continue
		}
		var body struct {
			Tags []string `json:"tags"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("the registry answered %s", res.Status)
		}
		if err != nil {
			return "", errors.New("the registry's answer is not a list of tags")
		}
		for _, tag := range body.Tags {
			if selfupdate.IsRelease(tag) && (found == "" || selfupdate.Newer(tag, found)) {
				found = tag
			}
		}
		// More tags than one answer holds: the registry says where the
		// rest is.
		next = ""
		if link := res.Header.Get("Link"); strings.Contains(link, `rel="next"`) {
			if from, to := strings.Index(link, "<"), strings.Index(link, ">"); from >= 0 && to > from {
				if ref, err := url.Parse(link[from+1 : to]); err == nil {
					next = res.Request.URL.ResolveReference(ref).String()
				}
			}
		}
	}
	if found == "" {
		return "", errors.New("no releases are published yet")
	}
	return found, nil
}

func registryGet(ctx context.Context, client *http.Client, address, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	return client.Do(req)
}

var challengeField = regexp.MustCompile(`(\w+)="([^"]*)"`)

// registryToken gets the anonymous token a registry asks for before it
// shows a public repository (the Docker token flow).
func registryToken(ctx context.Context, client *http.Client, challenge string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return "", errors.New("the registry does not show this repository without an account")
	}
	fields := map[string]string{}
	for _, m := range challengeField.FindAllStringSubmatch(challenge, -1) {
		fields[m[1]] = m[2]
	}
	realm, err := url.Parse(fields["realm"])
	if err != nil || (realm.Scheme != "https" && realm.Scheme != "http") || realm.Host == "" {
		return "", errors.New("the registry does not say where to get access")
	}
	q := realm.Query()
	for _, k := range []string{"service", "scope"} {
		if fields[k] != "" {
			q.Set(k, fields[k])
		}
	}
	realm.RawQuery = q.Encode()
	res, err := registryGet(ctx, client, realm.String(), "")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil || res.StatusCode != http.StatusOK {
		return "", errors.New("the registry does not show this repository without an account")
	}
	if body.Token == "" {
		body.Token = body.AccessToken
	}
	if body.Token == "" {
		return "", errors.New("the registry does not show this repository without an account")
	}
	return body.Token, nil
}
