// Package web embeds the built dashboard (web/dist) into the Go binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built SPA files (may contain only .gitkeep before `npm run build`).
func Dist() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}
