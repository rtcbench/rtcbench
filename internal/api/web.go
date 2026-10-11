package api

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"html"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) {
	if s.webProxy != nil {
		s.webProxy.ServeHTTP(w, r)
		return
	}
	if s.Web == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	name := strings.TrimPrefix(r.URL.Path, "/")
	if data, err := fs.ReadFile(s.Web, name+".gz"); err == nil {
		serveCompressedAsset(w, r, name, data)
		return
	}
	if info, err := fs.Stat(s.Web, name); err != nil || info.IsDir() {
		name = "index.html"
	}
	if name == "index.html" {
		s.serveIndex(w, r)
		return
	}
	data, err := fs.ReadFile(s.Web, name)
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	serveAsset(w, r, name, data)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.Web, "index.html")
	if err != nil {
		http.Error(w, "app unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method != http.MethodHead {
		_, _ = w.Write(s.webIndex(data, r))
	}
}

func serveCompressedAsset(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	w.Header().Add("Vary", "Accept-Encoding")
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	if acceptsGzip(strings.Join(r.Header.Values("Accept-Encoding"), ",")) {
		w.Header().Set("Content-Encoding", "gzip")
		serveAsset(w, r, name, data)
		return
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	defer reader.Close()
	data, err = io.ReadAll(reader)
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	serveAsset(w, r, name, data)
}

func (s *Server) webIndex(data []byte, r *http.Request) []byte {
	page := string(data)
	for _, asset := range []string{"main.js", "styles.css"} {
		page = strings.ReplaceAll(page, `"`+asset+`"`, `"`+asset+`?v=`+html.EscapeString(url.QueryEscape(s.version))+`"`)
	}
	if prefix := r.Header.Get("X-Forwarded-Prefix"); strings.HasPrefix(prefix, "/") && !strings.HasPrefix(prefix, "//") && !strings.ContainsAny(prefix, `\?#`) {
		page = strings.Replace(page, `<base href="/"`, `<base href="`+html.EscapeString(strings.TrimRight(prefix, "/")+"/")+`"`, 1)
	}
	if s.settings.Welcome {
		page = strings.Replace(page, `data-welcome="false"`, `data-welcome="true"`, 1)
	}
	return []byte(page)
}

func serveAsset(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	w.Header().Set("ETag", fmt.Sprintf(`"%x"`, sha256.Sum256(data)))
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func acceptsGzip(header string) bool {
	wildcard := false
	for value := range strings.SplitSeq(header, ",") {
		coding, params, err := mime.ParseMediaType(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		quality := 1.0
		if q, ok := params["q"]; ok {
			quality, err = strconv.ParseFloat(q, 64)
		}
		accepted := err == nil && quality > 0 && quality <= 1
		if coding == "gzip" {
			return accepted
		}
		if coding == "*" {
			wildcard = accepted
		}
	}
	return wildcard
}
