package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"time"
)

func (s *Server) loadBuildContext(ctx context.Context, plugin string) (map[string]string, error) {
	name := "docker-" + plugin + ".json"
	body, err := s.openWebAsset(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("load %s from UI: %w", name, err)
	}
	defer body.Close()
	const maxSize = 4 << 20
	data, err := io.ReadAll(io.LimitReader(body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, errors.New("UI build context exceeds 4 MiB")
	}
	var files map[string]string
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", name, err)
	}
	return files, nil
}

func (s *Server) openWebAsset(ctx context.Context, name string) (io.ReadCloser, error) {
	if s.settings.UI.Address != "" {
		address, err := url.JoinPath(s.settings.UI.Address, name)
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		client := http.Client{Timeout: 30 * time.Second}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("UI returned %s", response.Status)
		}
		return response.Body, nil
	}
	if s.Web == nil {
		return nil, errors.New("configure ui.address or use a build with embedded UI assets")
	}
	if data, err := fs.ReadFile(s.Web, name+".gz"); err == nil {
		return gzip.NewReader(bytes.NewReader(data))
	}
	return s.Web.Open(name)
}
