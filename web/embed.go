// Package web holds the built UI. `npm run build` writes it to dist/ui, the Makefile and the
// Dockerfile build it before the Go binary; without a build the server answers that the UI is
// not built.
package web

import (
	"embed"
	"io/fs"
)

// dist/.gitkeep keeps the pattern valid before the first build.
//
//go:embed all:dist
var dist embed.FS

// FS returns the built UI, or nil when there is no build.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist/ui")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
