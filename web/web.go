// Package web holds the embedded templates and static assets.
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var content embed.FS

// Templates is the template directory.
func Templates() fs.FS {
	sub, err := fs.Sub(content, "templates")
	if err != nil {
		panic(err)
	}
	return sub
}

// Static is the static asset directory mounted at /static/.
func Static() fs.FS {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
