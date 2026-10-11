package janus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rtcbench/rtcbench"
	"github.com/rtcbench/rtcbench/pkg/log"
)

type setupEnv struct {
	rtcbench.PluginEnv
	cfg *rtcbench.Config
}

func (e setupEnv) Config() *rtcbench.Config   { return e.cfg }
func (e setupEnv) LogRegistry() *log.Registry { return log.NewRegistry(nil, nil) }

func TestSetupOptionalHTTPSFlag(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tls         bool
		flag        any
		wantRequest bool
	}{
		{"omitted HTTP", false, nil, true},
		{"omitted HTTPS rejects untrusted certificate", true, nil, false},
		{"explicit false rejects untrusted certificate", true, false, false},
		{"explicit true permits self-signed HTTPS", true, true, true},
		{"invalid flag returns error", false, "true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requested atomic.Bool
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested.Store(true)
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			}))
			if tc.tls {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			defer srv.Close()
			settings := map[string]any{cfgServerRoot: srv.URL}
			if tc.flag != nil {
				settings[cfgAllowInsecure] = tc.flag
			}
			cfg := &rtcbench.Config{}
			cfg.Spec.PluginConfig = map[string]any{PluginID: settings}
			err := (&Plugin{}).Setup(context.Background(), setupEnv{cfg: cfg})
			if err == nil || requested.Load() != tc.wantRequest {
				t.Fatalf("Setup error = %v, request sent = %v, want %v", err, requested.Load(), tc.wantRequest)
			}
		})
	}
}
