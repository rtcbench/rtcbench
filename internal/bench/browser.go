package bench

import (
	"encoding/json"
	"fmt"
)

func BrowserURL(c Cluster, t Topology, cell Cell, mode string) string {
	plugin, err := c.plugin(cell.SFU, "")
	if err != nil {
		return ""
	}
	return plugin.BrowserURL(t.SFUIP, mode == "sender")
}

func roomHandler(perRoom int, baseURL string) []byte {
	encoded, _ := json.Marshal(baseURL)
	return []byte(fmt.Sprintf(`module.exports = function({id}) {
  const room = 1234 + Math.floor(id / %d);
  const url = new URL(%s);
  for (const key of ["room", "roomId"]) {
    if (url.searchParams.has(key)) {
      const value = url.searchParams.get(key);
      url.searchParams.set(key, /^\d+$/.test(value) ? String(room) : "room-" + room);
      return url.href;
    }
  }
  url.pathname = url.pathname.replace(/room-\d+/, "room-" + room);
  return url.href;
};
`, perRoom, encoded))
}

type browserSettings struct {
	URL                 string `json:"url"`
	Sessions            int    `json:"sessions"`
	MaxVideoDecoders    int    `json:"maxVideoDecoders"`
	ShowPageLog         bool   `json:"showPageLog"`
	StatsInterval       int    `json:"statsInterval"`
	StatsPath           string `json:"statsPath"`
	DetailedStatsPath   string `json:"detailedStatsPath"`
	EnableDetailedStats string `json:"enableDetailedStats"`
	CustomURLHandler    string `json:"customUrlHandler"`
	RunDuration         int    `json:"runDuration"`
	VideoPath           string `json:"videoPath"`
}

func browserConfig(c Cluster, t Topology, cell Cell, mode, dir string) ([]byte, error) {
	decoders := -1
	if cell.Client == "webrtcperf" {
		decoders = 0
	}
	config := browserSettings{
		URL:                 BrowserURL(c, t, cell, mode),
		Sessions:            t.Load,
		MaxVideoDecoders:    decoders,
		ShowPageLog:         true,
		StatsInterval:       c.SampleSeconds,
		StatsPath:           dir + "/stats/wrp-stats.csv",
		CustomURLHandler:    dir + "/rooms.js",
		DetailedStatsPath:   dir + "/stats/wrp-tracks.csv",
		EnableDetailedStats: fmt.Sprintf("0-%d", t.Load-1),
		RunDuration:         c.StartupSeconds + c.WarmupSeconds + c.DurationSeconds + c.StartLeadSeconds + 600,
	}
	if mode == "sender" {
		config.VideoPath = c.VideoPath
	}
	return json.MarshalIndent(config, "", "  ")
}
