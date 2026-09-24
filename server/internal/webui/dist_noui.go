//go:build noui

package webui

import "io/fs"

// Dist returns nil: this binary was built without the frontend.
func Dist() fs.FS { return nil }
