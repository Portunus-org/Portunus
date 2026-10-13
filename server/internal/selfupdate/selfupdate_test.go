package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The updater is told a version and nothing else. Whatever arrives in that
// one field, it can only become "the same image, a newer release".
func TestOnlyANewerReleaseIsAllowed(t *testing.T) {
	for _, c := range []struct {
		want, current string
		ok            bool
	}{
		{"v0.2.6", "v0.2.5", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "latest", true}, // a container that follows "latest"
		{"v1.0.0", "local", true},  // a local build
		{"v0.2.5", "v0.2.5", false},
		{"v0.2.4", "v0.2.5", false}, // never back: an old release may have holes
		{"latest", "v0.2.5", false},
		{"v0.2.6-rc1", "v0.2.5", false},
		{"v0.2.6 --privileged", "v0.2.5", false},
		{"v0.2.6@sha256:abc", "v0.2.5", false},
		{"evil/image:v9.9.9", "v0.2.5", false},
		{"../v1.0.0", "v0.2.5", false},
		{"", "v0.2.5", false},
	} {
		if err := Allowed(c.want, c.current); (err == nil) != c.ok {
			t.Errorf("%q over %q: allowed = %v, want %v", c.want, c.current, err == nil, c.ok)
		}
	}
}

func TestImageReferences(t *testing.T) {
	for ref, want := range map[string][2]string{
		"ghcr.io/paulichp/portunus:v0.2.5":            {"ghcr.io/paulichp/portunus", "v0.2.5"},
		"ghcr.io/paulichp/portunus":                   {"ghcr.io/paulichp/portunus", ""},
		"localhost:5000/portunus-node:latest":         {"localhost:5000/portunus-node", "latest"},
		"localhost:5000/portunus-node":                {"localhost:5000/portunus-node", ""},
		"portunus:local":                              {"portunus", "local"},
		"ghcr.io/paulichp/portunus:v0.2.5@sha256:abc": {"ghcr.io/paulichp/portunus", "v0.2.5"},
		"ghcr.io/paulichp/portunus@sha256:abc":        {"ghcr.io/paulichp/portunus", ""},
	} {
		if got := [2]string{Repository(ref), Tag(ref)}; got != want {
			t.Errorf("%s: %v, want %v", ref, got, want)
		}
	}
}

// A request that is not a release is not written and, should one get into
// the file some other way, is not read.
func TestRequestsAndSignsOfLife(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := Ask(dir, "latest", now); err == nil {
		t.Fatal("a request for something that is not a release was written")
	}
	if _, ok := ReadRequest(dir); ok {
		t.Fatal("a request appeared")
	}
	os.WriteFile(filepath.Join(dir, "request.json"), []byte(`{"version":"v1.0.0; rm -rf /","at":"2026-01-01T00:00:00Z"}`), 0o644)
	if _, ok := ReadRequest(dir); ok {
		t.Fatal("a request that is not a release was read")
	}
	if err := Ask(dir, "v1.2.3", now); err != nil {
		t.Fatal(err)
	}
	if r, ok := ReadRequest(dir); !ok || r.Version != "v1.2.3" {
		t.Fatalf("request: %+v %v", r, ok)
	}

	// No updater, a live one, and one that fell silent.
	if _, there := Look(dir, now); there {
		t.Fatal("an updater is reported where there is none")
	}
	writeFile(dir, "status.json", Status{At: now.UTC(), State: StateIdle})
	if st, there := Look(dir, now.Add(20*time.Second)); !there || st.State != StateIdle {
		t.Fatalf("a live updater is not seen: %+v %v", st, there)
	}
	if _, there := Look(dir, now.Add(10*time.Minute)); there {
		t.Fatal("an updater that stopped long ago still counts")
	}
}

// The new container is the old one in everything it was given when it was
// made, and takes from the new image what the old one only had because its
// image said so.
func TestTheNewContainerIsLikeTheOld(t *testing.T) {
	old := container{
		ID: "abcdef123456789",
		Config: obj{
			"Image":      "ghcr.io/paulichp/portunus-node:v0.2.5",
			"Hostname":   "abcdef123456",
			"Env":        []any{"PORTUNUS_JOIN=token", "PATH=/usr/local/bin", "GOMEMLIMIT=32MiB"},
			"Entrypoint": []any{"portunus-node"},
			"Cmd":        nil,
			"User":       "10001:10001",
			"Labels":     obj{"com.docker.compose.project": "portunus", "org.opencontainers.image.version": "v0.2.5"},
		},
		HostConfig: obj{"NetworkMode": "host", "ReadonlyRootfs": true, "CapDrop": []any{"ALL"}},
	}
	oldImage := obj{
		"Env":        []any{"PATH=/usr/local/bin", "GOMEMLIMIT=32MiB"},
		"Entrypoint": []any{"portunus-node"},
		"Cmd":        nil,
		"User":       "10001:10001",
		"Labels":     obj{"org.opencontainers.image.version": "v0.2.5"},
	}
	body := sameAs(old, oldImage, "ghcr.io/paulichp/portunus-node:v0.2.6")
	if body["Image"] != "ghcr.io/paulichp/portunus-node:v0.2.6" {
		t.Fatalf("image: %v", body["Image"])
	}
	if fmt.Sprint(body["Env"]) != "[PORTUNUS_JOIN=token]" {
		t.Fatalf("environment: %v (what was given must stay, what the image set must go)", body["Env"])
	}
	for _, k := range []string{"Entrypoint", "User", "Hostname"} {
		if _, kept := body[k]; kept {
			t.Errorf("%s was carried over from the old image", k)
		}
	}
	if l := body["Labels"].(obj); l["com.docker.compose.project"] != "portunus" || len(l) != 1 {
		t.Fatalf("labels: %v", l)
	}
	if hc := body["HostConfig"].(obj); hc["ReadonlyRootfs"] != true || fmt.Sprint(hc["CapDrop"]) != "[ALL]" || hc["NetworkMode"] != "host" {
		t.Fatalf("the restrictions of the container were lost: %v", hc)
	}

	// An updater: its command was given when it was made and must stay.
	up := container{ID: "ffff", Config: obj{"Image": "portunus-node:v1", "Cmd": []any{"updater", "portunus"}, "User": "0", "Entrypoint": []any{"portunus-node"}}, HostConfig: obj{"NetworkMode": "none"}}
	body = sameAs(up, oldImage, "portunus-node:v2")
	if fmt.Sprint(body["Cmd"]) != "[updater portunus]" || body["User"] != "0" {
		t.Fatalf("the updater lost its command or its user: %v", body)
	}
}
