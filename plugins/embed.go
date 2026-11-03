// Package plugins embeds Genkan's built-in example plugins (catalog/*.js).
// go:embed cannot reach parent directories, so the files live next to this
// file and are exported here. See docs/PLUGINS.md §8.
package plugins

import (
	"embed"
	"io/fs"
)

//go:embed all:catalog
var files embed.FS

// Catalog returns the example plugins as a flat file system of *.js files.
func Catalog() fs.FS {
	sub, err := fs.Sub(files, "catalog")
	if err != nil { // embedded at build time: cannot happen
		panic(err)
	}
	return sub
}
