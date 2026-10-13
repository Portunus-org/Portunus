package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PaulichP/Portunus/server/internal/selfupdate"
	"github.com/PaulichP/Portunus/server/internal/version"
)

func updaterSays(t *testing.T, dir string, st selfupdate.Status) {
	t.Helper()
	st.At = time.Now().UTC()
	raw := []byte(`{"at":"` + st.At.Format(time.RFC3339Nano) + `","state":"` + st.State + `","version":"` + st.Version + `","error":"` + st.Error + `"}`)
	if err := os.WriteFile(filepath.Join(dir, "status.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func refusal(err error) string {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.Code
	}
	return ""
}

// The button in the panel does one thing: it leaves the number of the
// newest release for the updater. It does that only when there is an
// updater, something to install, and updates are not turned off.
func TestInstallingFromThePanel(t *testing.T) {
	defer func(v string) { version.Version = v }(version.Version)
	version.Version = "v0.2.5"
	ctx := context.Background()
	c, _ := newBindingCore(t)
	dir := t.TempDir()
	c.updates.dir, c.updates.before = dir, filepath.Join(t.TempDir(), "before-update.db")
	c.updates.state.Latest, c.updates.state.Available = "v0.2.6", true

	if st := c.UpdateState(ctx); st.Auto || st.Updater || st.Weekday != -1 || st.Minute != 180 || st.Planned != nil {
		t.Fatalf("defaults: %+v", st)
	}
	if code := refusal(c.InstallUpdate(ctx)); code != "no_updater" {
		t.Fatalf("without an updater: %q", code)
	}
	updaterSays(t, dir, selfupdate.Status{State: selfupdate.StateIdle})
	if c.SetUpdateSchedule(ctx, UpdateSchedule{Weekday: 7}) == nil || c.SetUpdateSchedule(ctx, UpdateSchedule{Weekday: -1, Minute: 1440}) == nil {
		t.Fatal("a schedule that makes no sense was saved")
	}
	c.updates.state.Available = false
	if code := refusal(c.InstallUpdate(ctx)); code != "nothing_to_install" {
		t.Fatalf("nothing to install: %q", code)
	}
	if _, asked := selfupdate.ReadRequest(dir); asked {
		t.Fatal("a refused install left a request")
	}

	c.updates.state.Available = true
	if err := c.InstallUpdate(ctx); err != nil {
		t.Fatal(err)
	}
	if req, ok := selfupdate.ReadRequest(dir); !ok || req.Version != "v0.2.6" {
		t.Fatalf("the request: %+v %v", req, ok)
	}
	if st, err := os.Stat(c.updates.before); err != nil || st.Size() == 0 {
		t.Fatalf("no copy of the database was made before the update: %v", err)
	}
	if st := c.UpdateState(ctx); st.Installing != "v0.2.6" || st.InstallError != "" {
		t.Fatalf("while it installs: %+v", st)
	}
	// It failed: the reason is shown, and it no longer counts as installing.
	updaterSays(t, dir, selfupdate.Status{State: selfupdate.StateFailed, Version: "v0.2.6", Error: "the new version does not start"})
	if st := c.UpdateState(ctx); st.Installing != "" || st.InstallError == "" {
		t.Fatalf("after a failure: %+v", st)
	}
	// A failure of the release that now runs is history, not news.
	version.Version = "v0.2.6"
	if st := c.UpdateState(ctx); st.InstallError != "" || st.Installing != "" {
		t.Fatalf("after moving on: %+v", st)
	}
}

func TestUpdatesOnASchedule(t *testing.T) {
	defer func(v string) { version.Version = v }(version.Version)
	version.Version = "v0.2.5"
	ctx := context.Background()
	c, _ := newBindingCore(t)
	dir := t.TempDir()
	c.updates.dir = dir
	c.updates.state.Latest, c.updates.state.Available = "v0.2.6", true
	updaterSays(t, dir, selfupdate.Status{State: selfupdate.StateIdle})
	asked := func() bool { _, ok := selfupdate.ReadRequest(dir); return ok }
	forget := func() { os.Remove(filepath.Join(dir, "request.json")) }
	// 13 October 2026 is a Tuesday.
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC) }

	// By the button: no hour means anything.
	c.installOnSchedule(ctx, at(13, 3, 30))
	if asked() {
		t.Fatal("installed by itself though updates wait for the button")
	}

	// By themselves, on Wednesdays at 04:30.
	if err := c.SetUpdateSchedule(ctx, UpdateSchedule{Auto: true, Weekday: 3, Minute: 4*60 + 30}); err != nil {
		t.Fatal(err)
	}
	for _, early := range []time.Time{at(13, 4, 45), at(14, 4, 29), at(14, 5, 31), at(15, 4, 45)} {
		c.installOnSchedule(ctx, early)
		if asked() {
			t.Fatalf("installed at %s, outside its hour", early.Format("Mon 15:04"))
		}
	}
	c.installOnSchedule(ctx, at(14, 4, 45))
	if !asked() {
		t.Fatal("not installed in its hour")
	}
	// It failed and was cleared away: the same release is not tried again
	// every minute of that hour.
	forget()
	c.installOnSchedule(ctx, at(14, 4, 50))
	if asked() {
		t.Fatal("the same release was asked for twice")
	}
	// A later release is another matter. And an hour that runs past
	// midnight belongs to the day it began on.
	c.updates.state.Latest = "v0.2.7"
	c.SetUpdateSchedule(ctx, UpdateSchedule{Auto: true, Weekday: 3, Minute: 23*60 + 30})
	c.installOnSchedule(ctx, at(15, 0, 10)) // Thursday, ten past midnight
	if req, ok := selfupdate.ReadRequest(dir); !ok || req.Version != "v0.2.7" {
		t.Fatal("the next release was not installed in the hour that began on its day")
	}
}

// A moment can be chosen for the release that waits: a quiet day, a quiet
// hour. It is installed then, once, whether updates install themselves or
// not.
func TestAnUpdatePlannedForAMoment(t *testing.T) {
	defer func(v string) { version.Version = v }(version.Version)
	version.Version = "v0.2.5"
	ctx := context.Background()
	c, now := newBindingCore(t)
	dir := t.TempDir()
	c.updates.dir = dir
	asked := func() bool { _, ok := selfupdate.ReadRequest(dir); return ok }
	when := now.Add(50 * time.Hour)

	// Nothing to plan without an updater or without a release.
	c.updates.state.Latest, c.updates.state.Available = "v0.2.6", true
	if code := refusal(c.PlanUpdate(ctx, &when)); code != "no_updater" {
		t.Fatalf("without an updater: %q", code)
	}
	updaterSays(t, dir, selfupdate.Status{State: selfupdate.StateIdle})
	c.updates.state.Available = false
	if code := refusal(c.PlanUpdate(ctx, &when)); code != "nothing_to_install" {
		t.Fatalf("with nothing to install: %q", code)
	}
	c.updates.state.Available = true
	past, far := now.Add(-time.Minute), now.Add(200*24*time.Hour)
	if refusal(c.PlanUpdate(ctx, &past)) != "in_the_past" || refusal(c.PlanUpdate(ctx, &far)) != "too_far" {
		t.Fatal("a moment that makes no sense was accepted")
	}

	if err := c.PlanUpdate(ctx, &when); err != nil {
		t.Fatal(err)
	}
	if st := c.UpdateState(ctx); st.Planned == nil || !st.Planned.Equal(when) {
		t.Fatalf("the plan is not shown: %+v", st.Planned)
	}
	c.installOnSchedule(ctx, when.Add(-time.Minute))
	if asked() {
		t.Fatal("installed before the planned moment")
	}
	// Taken back: the moment passes and nothing happens.
	if err := c.PlanUpdate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	c.installOnSchedule(ctx, when.Add(time.Minute))
	if asked() || c.UpdateState(ctx).Planned != nil {
		t.Fatal("a plan that was taken back was carried out")
	}
	// Planned again, and the moment comes.
	c.PlanUpdate(ctx, &when)
	c.installOnSchedule(ctx, when.Add(time.Minute))
	if !asked() {
		t.Fatal("not installed at the planned moment")
	}
	if c.UpdateState(ctx).Planned != nil {
		t.Fatal("the plan stayed after it was carried out")
	}
}
