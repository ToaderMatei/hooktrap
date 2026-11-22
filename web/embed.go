// Package web embeds the dashboard's static assets.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var embedded embed.FS

// FS returns the dashboard file system rooted at the static directory.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
