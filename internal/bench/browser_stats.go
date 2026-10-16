package bench

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

type browserStream struct {
	time, bitrate, fps float64
}

func browserReceiverQuality(reader io.Reader, expected int, cfg Cluster, decode bool, since float64) (Quality, error) {
	r := csv.NewReader(reader)
	header, err := r.Read()
	if err != nil {
		return Quality{}, fmt.Errorf("browser track header: %w", err)
	}
	columns := map[string]int{}
	for i, name := range header {
		columns[name] = i
	}
	required := []string{"datetime", "participantName", "trackId", "videoRecvBitrates"}
	if decode {
		required = append(required, "videoRecvFrameRate")
	}
	for _, name := range required {
		if _, ok := columns[name]; !ok {
			return Quality{}, fmt.Errorf("browser track stats missing %s", name)
		}
	}
	number := func(row []string, name string) (float64, error) {
		value := row[columns[name]]
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, fmt.Errorf("invalid browser track stat %s=%q", name, value)
		}
		return n, nil
	}
	pages := map[string]map[string]browserStream{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Quality{}, fmt.Errorf("browser track stats: %w", err)
		}
		timestamp, err := number(row, "datetime")
		if err != nil {
			return Quality{}, err
		}
		if timestamp < since*1000 || row[columns["videoRecvBitrates"]] == "" {
			continue
		}
		page, track := row[columns["participantName"]], row[columns["trackId"]]
		if page == "" || track == "" {
			return Quality{}, fmt.Errorf("browser video stats missing participant or track")
		}
		bitrate, err := number(row, "videoRecvBitrates")
		if err != nil {
			return Quality{}, err
		}
		fps := 0.0
		if decode && row[columns["videoRecvFrameRate"]] != "" {
			fps, err = number(row, "videoRecvFrameRate")
			if err != nil {
				return Quality{}, err
			}
		}
		if pages[page] == nil {
			pages[page] = map[string]browserStream{}
		}
		if timestamp >= pages[page][track].time {
			pages[page][track] = browserStream{timestamp, bitrate, fps}
		}
	}
	q := Quality{Expected: expected, Reported: len(pages), MinFPS: math.Inf(1), MinBitrate: math.Inf(1)}
	for _, tracks := range pages {
		var selected browserStream
		for _, stream := range tracks {
			if stream.bitrate > selected.bitrate || (stream.bitrate == selected.bitrate && stream.time > selected.time) {
				selected = stream
			}
		}
		q.MinBitrate = min(q.MinBitrate, selected.bitrate)
		q.MinFPS = min(q.MinFPS, selected.fps)
		if selected.bitrate >= cfg.MinBitrate && (!decode || selected.fps >= cfg.MinFPS) {
			q.Healthy++
		}
	}
	if len(pages) == 0 {
		q.MinBitrate, q.MinFPS = 0, 0
	}
	return finishQuality(q, cfg.HealthyFraction), nil
}

func (e *experiment) browserQuality(ctx context.Context, c container, expected int) (Quality, error) {
	freshness := max(15, e.cluster.SampleSeconds*3)
	file := quote(c.stats + "/wrp-tracks.csv")
	command := fmt.Sprintf(
		"now=$(date +%%s); printf '%%s\\n' \"$now\"; "+
			"awk -F, -v since=\"$(( (now-%d)*1000 ))\" "+
			"'NR==1 || $1>=since' %s 2>/dev/null || test ! -f %s",
		freshness, file, file,
	)
	data, err := e.readRemote(ctx, c.host, command)
	if err != nil {
		return Quality{}, err
	}
	clock, data, ok := strings.Cut(data, "\n")
	now, err := strconv.ParseFloat(strings.TrimSpace(clock), 64)
	if !ok || err != nil {
		return Quality{}, fmt.Errorf("invalid browser stats clock")
	}
	if strings.TrimSpace(data) == "" {
		return finishQuality(Quality{Expected: expected}, e.cluster.HealthyFraction), nil
	}
	return browserReceiverQuality(strings.NewReader(data), expected, e.cluster, e.cell.Client == "chromium", now-float64(freshness))
}
