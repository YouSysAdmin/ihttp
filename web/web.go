// Package web embeds the built Vue console so the binary can serve it.
//
// Only a .gitkeep is committed under dist/, so a plain `go build` with
// no `npm run build` still compiles - Available() then reports false and
// the console route explains itself instead of serving a shell that
// points at scripts the binary does not carry.
package web

import (
	"embed"
	"io/fs"
)

// Frontend is the built console, compiled into the binary.
//
// The embed pattern needs at least one match or the build fails, which
// is why dist/.gitkeep is committed and why `task web` puts it back
// after the build empties the directory.
//
//go:embed all:dist
var Frontend embed.FS

// FS returns the console root, or nil when no build was embedded.
func FS() fs.FS {
	if !Available() {
		return nil
	}

	sub, err := fs.Sub(Frontend, "dist")
	if err != nil {
		return nil
	}

	return sub
}

// Available reports whether a real build was embedded. The marker is
// index.html: the placeholder alone is not a console.
func Available() bool {
	f, err := Frontend.Open("dist/index.html")
	if err != nil {
		return false
	}

	_ = f.Close()

	return true
}
