package db

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// ErrNewer is returned by Open for a database written by a newer version.
var ErrNewer = errors.New("the database is from a newer version")

// BeforeUpdate is how long the copy of the database made before an update
// stands for "what this version left": older than that, it is somebody
// else's past and is not put back.
const BeforeUpdate = 24 * time.Hour

// PutBack restores the copy of the database made just before an update.
// An update that changed the database and then did not come up is undone by
// starting the previous version again, and that version cannot read what
// the new one left. The database it cannot read is moved aside, not
// deleted. It reports whether the copy was put in place.
func PutBack(path, copyPath string, now time.Time) (bool, error) {
	st, err := os.Stat(copyPath)
	if err != nil || now.Sub(st.ModTime()) > BeforeUpdate {
		return false, nil
	}
	aside := path + ".newer"
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(aside + suffix)
		if err := os.Rename(path+suffix, aside+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("move the newer database aside: %w", err)
		}
	}
	src, err := os.Open(copyPath)
	if err != nil {
		return false, err
	}
	defer src.Close()
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return false, err
	}
	return true, dst.Close()
}
