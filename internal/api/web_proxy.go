package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

func (s *Server) configureWeb() error {
	if s.settings.UI.Address == "" {
		return nil
	}
	address, err := url.Parse(s.settings.UI.Address)
	if err != nil || address.Hostname() == "" || address.User != nil || address.Fragment != "" || (address.Scheme != "http" && address.Scheme != "https") {
		return errors.New("ui.address must be an HTTP or HTTPS URL")
	}
	s.webProxy = &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(address)
			request.SetXForwarded()
			request.Out.Header.Set("Accept-Encoding", "identity")
		},
		ModifyResponse: func(response *http.Response) error {
			if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") {
				return nil
			}
			data, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				return err
			}
			data = s.webIndex(data, response.Request)
			response.Body = io.NopCloser(bytes.NewReader(data))
			response.ContentLength = int64(len(data))
			response.Header.Set("Content-Length", strconv.Itoa(len(data)))
			response.Header.Del("ETag")
			return nil
		},
	}
	return nil
}
