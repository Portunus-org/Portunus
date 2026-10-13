package selfupdate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Options is what an updater is started with.
type Options struct {
	// Target is the name of the container to keep up to date.
	Target string
	// Dir is the directory shared with that container.
	Dir string
	// Socket is the Docker daemon's socket.
	Socket string
	Log    *slog.Logger

	every, patience, steady time.Duration
}

// Run is the updater: it installs what is asked for in Dir until ctx ends.
func Run(ctx context.Context, o Options) error {
	if o.Target == "" {
		return fmt.Errorf("say which container to look after: updater <name>")
	}
	if o.every == 0 {
		o.every, o.patience, o.steady = 10*time.Second, 2*time.Minute, 10*time.Second
	}
	u := &updater{Options: o, d: newDocker(o.Socket)}
	if st, err := os.Stat(o.Dir); err != nil || !st.IsDir() {
		return fmt.Errorf("%s, the directory shared with %s, is not there", o.Dir, o.Target)
	}
	u.takeOver(ctx)
	o.Log.Info("updater started", "target", o.Target)
	for {
		u.look(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(o.every):
		}
	}
}

type updater struct {
	Options
	d *docker
	// failed is the request that could not be installed: it is not tried
	// again until another one comes.
	failed Request
	status Status
	// selfTried is the release this updater already tried to move itself to.
	selfTried string
}

func (u *updater) say(state, version, problem string) {
	u.status = Status{At: time.Now().UTC(), State: state, Version: version, Error: problem}
	if err := writeFile(u.Dir, "status.json", u.status); err != nil {
		u.Log.Error("write status", "err", err)
	}
}

func (u *updater) look(ctx context.Context) {
	req, ok := ReadRequest(u.Dir)
	if !ok || req == u.failed {
		// Nothing new: show a sign of life, keeping a failure on display.
		if u.status.State == "" {
			u.status.State = StateIdle
		}
		u.say(u.status.State, u.status.Version, u.status.Error)
		return
	}
	target, err := u.d.inspect(ctx, u.Target)
	if err != nil {
		u.say(StateFailed, req.Version, "the container to update was not found: "+err.Error())
		u.failed = req
		return
	}
	current := Tag(target.image())
	if current == req.Version {
		// Done, now or by an earlier run. The updater follows.
		u.say(StateIdle, "", "")
		u.follow(ctx, req.Version)
		return
	}
	if err := Allowed(req.Version, current); err != nil {
		u.say(StateFailed, req.Version, "refused: "+err.Error())
		u.failed = req
		return
	}
	u.say(StateUpdating, req.Version, "")
	u.Log.Info("updating", "target", u.Target, "from", current, "to", req.Version)
	if err := u.replace(ctx, target, req.Version); err != nil {
		u.Log.Error("update failed", "target", u.Target, "version", req.Version, "err", err)
		u.say(StateFailed, req.Version, err.Error())
		u.failed = req
		return
	}
	u.Log.Info("updated", "target", u.Target, "version", req.Version)
	u.say(StateIdle, "", "")
	u.follow(ctx, req.Version)
}

// replace puts a container made from release version in the place of old.
// The old one is kept, stopped, until the new one is seen to work, and is
// brought back if it does not.
func (u *updater) replace(ctx context.Context, old container, version string) error {
	repository := Repository(old.image())
	image := repository + ":" + version
	if err := u.d.fetch(ctx, repository, version); err != nil {
		return err
	}
	oldImage, err := u.d.imageConfig(ctx, old.Image)
	if err != nil {
		oldImage = obj{}
	}
	name, spare := old.name(), old.name()+"-old"
	if err := u.d.remove(ctx, spare); err != nil {
		return err
	}
	// The two cannot run side by side: they listen on the same ports.
	if err := u.d.stop(ctx, name); err != nil {
		return err
	}
	if err := u.d.rename(ctx, name, spare); err != nil {
		u.d.start(ctx, name)
		return err
	}
	back := func(why error) error {
		// Not tied to ctx: a shutdown must not leave the server with nothing.
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		u.d.remove(ctx, name)
		if err := u.d.rename(ctx, spare, name); err != nil {
			return fmt.Errorf("%w; and the previous version could not be put back: %v", why, err)
		}
		if err := u.d.start(ctx, name); err != nil {
			return fmt.Errorf("%w; and the previous version could not be started again: %v", why, err)
		}
		return fmt.Errorf("%w; the previous version is running again", why)
	}
	if err := u.d.create(ctx, name, sameAs(old, oldImage, image)); err != nil {
		return back(err)
	}
	if err := u.d.start(ctx, name); err != nil {
		return back(err)
	}
	if err := u.d.settle(ctx, name, u.patience, u.steady); err != nil {
		return back(err)
	}
	return u.d.remove(ctx, spare)
}

// self is the container this process runs in: its host name is its ID,
// unless it was given another, and then the updater leaves itself alone.
func (u *updater) self(ctx context.Context) (container, bool) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return container{}, false
	}
	c, err := u.d.inspect(ctx, host)
	if err != nil || !strings.HasPrefix(c.ID, host) {
		return container{}, false
	}
	return c, true
}

const next = "-next"

// follow moves the updater itself to the release it has just installed, so
// that it never needs a console either. A container cannot replace itself,
// so it starts its successor beside it and lets that one take its place
// (takeOver). If the successor does not come up, this one stays.
func (u *updater) follow(ctx context.Context, version string) {
	if u.selfTried == version {
		return
	}
	me, ok := u.self(ctx)
	if !ok || strings.HasSuffix(me.name(), next) || Tag(me.image()) == version || Allowed(version, Tag(me.image())) != nil {
		return
	}
	u.selfTried = version
	repository := Repository(me.image())
	if err := u.d.fetch(ctx, repository, version); err != nil {
		u.Log.Warn("the updater stays at its version", "err", err)
		return
	}
	myImage, err := u.d.imageConfig(ctx, me.Image)
	if err != nil {
		myImage = obj{}
	}
	successor := me.name() + next
	if err := u.d.remove(ctx, successor); err != nil {
		return
	}
	if err := u.d.create(ctx, successor, sameAs(me, myImage, repository+":"+version)); err != nil {
		u.Log.Warn("the updater stays at its version", "err", err)
		return
	}
	if err := u.d.start(ctx, successor); err != nil {
		u.d.remove(ctx, successor)
		u.Log.Warn("the updater stays at its version", "err", err)
		return
	}
	u.Log.Info("the updater hands over to its new version", "version", version)
}

// takeOver is the successor's first step: it removes the updater it was
// started by and takes its name.
func (u *updater) takeOver(ctx context.Context) {
	me, ok := u.self(ctx)
	if !ok || !strings.HasSuffix(me.name(), next) {
		return
	}
	name := strings.TrimSuffix(me.name(), next)
	if err := u.d.remove(ctx, name); err != nil {
		u.Log.Error("take over from the previous updater", "err", err)
		return
	}
	if err := u.d.rename(ctx, me.ID, name); err != nil {
		u.Log.Error("take over from the previous updater", "err", err)
		return
	}
	u.Log.Info("took over from the previous updater", "name", name)
}
