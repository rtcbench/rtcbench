package api

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCompressedWebAssets(t *testing.T) {
	const script = "console.log('rtcbench')"
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(script))
	_ = writer.Close()
	s, h := newServer(t)
	s.Web = fstest.MapFS{"main.js.gz": {Data: compressed.Bytes()}}
	for header, encoded := range map[string]bool{
		"": false, "identity": false, "br": false, "gzip": true,
		"br, gzip;q=0.5": true, "gzip;q=0": false, "xgzip": false,
		"*": true, "gzip;q=0, *": false, "*;q=0, gzip": true,
		"GZIP": true, "gzip;q=invalid": false,
	} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+"/"+header, func(t *testing.T) {
				headers := http.Header{"Accept-Encoding": {header}}
				w := webRequest(h, method, "/main.js?v=test", headers)
				if w.Code != 200 {
					t.Fatalf("status = %d, want 200", w.Code)
				}
				if got := w.Header().Get("Content-Encoding"); (got == "gzip") != encoded {
					t.Errorf("Content-Encoding = %q, want gzip: %t", got, encoded)
				}
				if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
					t.Errorf("Vary = %q, want Accept-Encoding", got)
				}
				if got := w.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
					t.Errorf("expected JavaScript Content-Type, got %q", got)
				}
				want := script
				if encoded {
					want = compressed.String()
				}
				if method == "HEAD" {
					want = ""
				}
				if w.Body.String() != want {
					t.Fatal("asset bytes differ")
				}
				etag := w.Header().Get("ETag")
				if etag == "" {
					t.Fatal("missing asset validator")
				}
				headers.Set("If-None-Match", etag)
				cached := webRequest(h, method, "/main.js?v=test", headers)
				if cached.Code != 304 || cached.Body.Len() != 0 {
					t.Fatal("unchanged asset was downloaded again", cached.Code)
				}
			})
		}
	}
}

func TestHTMLProxyPrefix(t *testing.T) {
	s, h := newServer(t)
	s.Web = fstest.MapFS{"index.html": {Data: []byte(`<base href="/">`)}}
	w := webRequest(h, "GET", "/plugins/new", http.Header{
		"Accept-Encoding":    {"gzip"},
		"X-Forwarded-Prefix": {"/app"},
	})
	if got := w.Body.String(); got != `<base href="/app/">` {
		t.Errorf("HTML = %q, want base href /app/", got)
	}
	for header, want := range map[string]string{
		"Content-Encoding": "",
		"ETag":             "",
		"Cache-Control":    "no-cache",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestUncompressedWebAssetRevalidation(t *testing.T) {
	s, h := newServer(t)
	files := fstest.MapFS{"main.js": {Data: []byte("original")}}
	s.Web = files
	headers := http.Header{}
	w := webRequest(h, "GET", "/main.js", headers)
	if w.Code != 200 || w.Body.String() != "original" || w.Header().Get("ETag") == "" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	headers.Set("If-None-Match", w.Header().Get("ETag"))
	w = webRequest(h, "GET", "/main.js", headers)
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("unchanged asset was downloaded again", w.Code)
	}
	files["main.js"].Data = []byte("updated")
	w = webRequest(h, "GET", "/main.js", headers)
	if w.Code != 200 || w.Body.String() != "updated" || w.Header().Get("ETag") == headers.Get("If-None-Match") {
		t.Fatal("stale asset reused", w.Code, w.Body.String())
	}
}

func webRequest(handler http.Handler, method, path string, headers http.Header) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	request.Header = headers
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
