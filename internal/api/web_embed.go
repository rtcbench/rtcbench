//go:build !cli

package api

import (
	"embed"
	"io/fs"
)

//go:embed all:web
var webFiles embed.FS

func defaultWeb() fs.FS {
	files, _ := fs.Sub(webFiles, "web")
	return files
}
