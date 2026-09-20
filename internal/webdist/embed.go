// Package webdist embeds the built frontend (Vite output in ./dist).
package webdist

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built frontend as a filesystem rooted at dist/.
func FS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
