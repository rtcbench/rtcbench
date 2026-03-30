package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"time"
)

const checkURL = "https://sfu-client-update-check.evancf.workers.dev/check"

type response struct {
	Latest          string `json:"latest"`
	PublishedAt     string `json:"published_at"`
	URL             string `json:"url"`
	UpdateAvailable bool   `json:"update_available"`
}

// Check contacts the update server and prints a colored notice to stderr
// if a newer version is available. It blocks for at most 2 seconds.
func Check(version, plugin string) {
	params := url.Values{
		"v":      {version},
		"os":     {runtime.GOOS},
		"arch":   {runtime.GOARCH},
		"plugin": {plugin},
	}

	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(checkURL + "?" + params.Encode())
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var r response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return
	}

	if !r.UpdateAvailable {
		return
	}

	fmt.Fprintf(
		// \033[1;36m = bold cyan, \033[0m = reset
		_stderr,
		"\033[1;36mA new version of call.zip is available: %s → %s\n  %s\033[0m\n",
		version, r.Latest, r.URL,
	)
}
