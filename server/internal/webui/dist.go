//go:build !noui

package webui

import (
	"embed"
	"io/fs"
)

// dist is the Next.js static export, copied here from web/out by
// `make web` (or the Dockerfile) before compiling. Build with -tags noui
// to run the API without a UI.
//
//go:embed all:dist
var dist embed.FS

// Dist returns the exported frontend rooted at its top directory.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
