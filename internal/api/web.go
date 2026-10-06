package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:web
var webFiles embed.FS

func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if info, err := fs.Stat(s.web, name); err != nil || info.IsDir() {
		name = "index.html"
	}
	http.ServeFileFS(w, r, s.web, name)
}
