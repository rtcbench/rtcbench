package bench

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

func TestMediasoupReadinessAcceptsOnlyExpectedResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("remote readiness uses Linux shell")
	}
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		command := strings.ReplaceAll(New(benchmark.Environment{}, DefaultConfig()).Services[0].Health, "https://localhost:4443", server.URL)
		err := exec.CommandContext(t.Context(), "sh", "-c", command).Run()
		server.Close()
		if (err == nil) != (status == http.StatusForbidden) {
			t.Errorf("HTTP %d: %v", status, err)
		}
	}
}
