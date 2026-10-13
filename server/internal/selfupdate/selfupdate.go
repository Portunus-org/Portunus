// Package selfupdate replaces a running container of the installation (the
// panel, or a node) with the same container made from a newer release, so
// that updating does not take a console.
//
// The work is done by the updater: a second container next to the one it
// looks after, started from the node image with the command "updater". It
// is the only part of the installation that may talk to Docker, which is as
// good as being root on that server, so it is kept as far from the outside
// as a process can be: it has no network at all and listens to nothing. Its
// one input is a file with a version number, written by the container next
// to it into a directory the two share. It turns that number into "the same
// image, this release": it cannot be told to run another image, another
// command or another container, and it never goes back to an older release.
package selfupdate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultDir is the directory the updater shares with the container it
// looks after.
const DefaultDir = "/run/portunus-update"

// alive is how fresh the updater's last sign of life must be for it to be
// counted on.
const alive = 90 * time.Second

// Request asks for a release. It is all the updater ever reads.
type Request struct {
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

// Status is what the updater says about itself.
type Status struct {
	// At is when the updater last looked.
	At time.Time `json:"at"`
	// State: "idle", "updating" or "failed".
	State string `json:"state"`
	// Version is the release being installed or the one that failed.
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

const (
	StateIdle     = "idle"
	StateUpdating = "updating"
	StateFailed   = "failed"
)

var release = regexp.MustCompile(`^v(\d{1,6})\.(\d{1,6})\.(\d{1,6})$`)

// IsRelease reports whether v names a release (three numbers, as v1.2.3).
func IsRelease(v string) bool { return release.MatchString(v) }

func numbers(v string) ([3]int, bool) {
	var n [3]int
	m := release.FindStringSubmatch(v)
	if m == nil {
		return n, false
	}
	for i := range n {
		n[i], _ = strconv.Atoi(m[i+1])
	}
	return n, true
}

// Newer reports whether release a is newer than release b; false when
// either is not a release.
func Newer(a, b string) bool {
	x, ok := numbers(a)
	y, ok2 := numbers(b)
	if !ok || !ok2 {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

// Allowed reports whether a container that runs current may be replaced by
// release want: want must be a release, and never an older or the same one.
// A build that is not a release (a local one) may move to any release.
func Allowed(want, current string) error {
	if !IsRelease(want) {
		return errors.New("not a release")
	}
	if IsRelease(current) && !Newer(want, current) {
		return errors.New("not newer than what runs now")
	}
	return nil
}

func writeFile(dir, name string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, name+".tmp")
	// Readable by the other container, which runs as another user.
	if err := os.WriteFile(tmp, raw, 0o644); err != nil { // nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission -- a version number, for the container next door to read
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

// Ask leaves a request for release version in dir.
func Ask(dir, version string, now time.Time) error {
	if !IsRelease(version) {
		return errors.New("not a release")
	}
	return writeFile(dir, "request.json", Request{Version: version, At: now.UTC()})
}

// ReadRequest returns the request waiting in dir, if any.
func ReadRequest(dir string) (Request, bool) {
	var r Request
	raw, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil || json.Unmarshal(raw, &r) != nil || !IsRelease(r.Version) {
		return Request{}, false
	}
	return r, true
}

// Look returns what the updater last said, and whether it is there at all:
// it shows itself every few seconds, so silence means there is no updater
// next to this container.
func Look(dir string, now time.Time) (Status, bool) {
	var s Status
	raw, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil || json.Unmarshal(raw, &s) != nil {
		return Status{}, false
	}
	return s, now.Sub(s.At) < alive && s.At.Sub(now) < alive
}

// Repository is an image reference without its tag or digest:
// "ghcr.io/owner/name:v1.2.3" gives "ghcr.io/owner/name".
func Repository(ref string) string {
	ref, _, _ = strings.Cut(ref, "@")
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref
}

// Tag is the tag of an image reference, "" when it has none.
func Tag(ref string) string {
	ref, _, _ = strings.Cut(ref, "@")
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		return ref[i+1:]
	}
	return ""
}
