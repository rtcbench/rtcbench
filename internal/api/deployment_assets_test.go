package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestBuildContextFromEmbeddedUI(t *testing.T) {
	data := []byte(`{"livekit/Dockerfile":"FROM custom-image"}`)
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	_, _ = w.Write(data)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ := newServer(t)
	for _, suffix := range []string{"", ".gz"} {
		asset := data
		if suffix != "" {
			asset = compressed.Bytes()
		}
		s.Web = fstest.MapFS{"docker-livekit.json" + suffix: {Data: asset}}
		files, err := s.deployments.LoadBuildContext(t.Context(), "livekit")
		if err != nil || files["livekit/Dockerfile"] != "FROM custom-image" {
			t.Fatal(files, err)
		}
	}
	s.Web = nil
	if _, err := s.loadBuildContext(t.Context(), "livekit"); err == nil {
		t.Fatal("loaded a context without a UI")
	}
}

func TestBuildContextFromUIAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ui/docker-janus.json" {
			t.Errorf("unexpected asset path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		_, _ = io.WriteString(writer, `{"janus/Dockerfile":"FROM hosted-image"}`)
		_ = writer.Close()
	}))
	defer upstream.Close()
	s, _ := newServer(t)
	s.settings.UI.Address = upstream.URL + "/ui/"
	s.Web = fstest.MapFS{"docker-janus.json": {Data: []byte("embedded context should not be used")}}
	files, err := s.deployments.LoadBuildContext(t.Context(), "janus")
	if err != nil || files["janus/Dockerfile"] != "FROM hosted-image" {
		t.Fatal(files, err)
	}
	s.Web = nil
	files, err = s.deployments.LoadBuildContext(t.Context(), "janus")
	if err != nil || files["janus/Dockerfile"] != "FROM hosted-image" {
		t.Fatal("CLI could not use external UI assets", files, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.loadBuildContext(ctx, "janus"); !errors.Is(err, context.Canceled) {
		t.Fatal("asset fetch ignored cancellation", err)
	}
}

func TestBuildContextErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "missing", status: http.StatusNotFound},
		{name: "HTML fallback", status: http.StatusOK, body: "<html>custom frontend</html>"},
		{name: "invalid file contents", status: http.StatusOK, body: `{"janus/Dockerfile":false}`},
		{name: "oversize", status: http.StatusOK, body: strings.Repeat("x", (4<<20)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer upstream.Close()
			s := &Server{}
			s.settings.UI.Address = upstream.URL
			if _, err := s.loadBuildContext(t.Context(), "janus"); err == nil {
				t.Fatal("invalid UI build context accepted")
			}
		})
	}
}
