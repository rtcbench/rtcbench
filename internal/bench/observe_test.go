package bench

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type unevenNativeReports struct {
	recordingRemote
	calls int
}

func (r *unevenNativeReports) Run(ctx context.Context, host, command string) (string, error) {
	if !strings.HasPrefix(command, "date +%s; tail") {
		return r.recordingRemote.Run(ctx, host, command)
	}
	r.calls++
	lines := 100
	if _, err := fmt.Sscanf(command, "date +%%s; tail -v -n %d", &lines); err != nil {
		lines = 100
	}
	var data strings.Builder
	data.WriteString("100\n")
	if strings.Contains(command, "tail -v") {
		data.WriteString("==> /tmp/stats/streams.jsonl <==\n")
	}
	if lines > 100 {
		data.WriteString("{\"ts\":90,\"viewer\":\"slow\",\"fps\":30,\"bitrate_bps\":3500000}\n")
	}
	for i := 0; i < 100; i++ {
		data.WriteString("{\"ts\":99,\"viewer\":\"fast\",\"fps\":30,\"bitrate_bps\":3500000}\n")
	}
	return data.String(), nil
}

func TestNativeMeasurementRetainsFreshSlowReports(t *testing.T) {
	c, _ := ParseCluster("")
	remote := &unevenNativeReports{}
	e := experiment{
		cluster:    c,
		remote:     remote,
		request:    Request{Mode: "receiver"},
		topology:   Topology{Load: 2},
		containers: []container{{host: "client", name: "native", stats: "/tmp/stats"}},
	}
	quality, err := e.measure(t.Context(), "client")
	if err != nil || !quality.Passed || quality.Reported != 2 || remote.calls != 2 {
		t.Fatalf("quality=%+v calls=%d err=%v", quality, remote.calls, err)
	}
}

func TestNativeTailChecksEachFile(t *testing.T) {
	data := "==> a.jsonl <==\n{\"ts\":80}\n{\"ts\":99}\n==> b.jsonl <==\n{\"ts\":98}\n{\"ts\":99}\n"
	_, complete, err := nativeTail(data, 2, 85)
	if err != nil || complete {
		t.Fatalf("unread fresh records in second file: complete=%t err=%v", complete, err)
	}
	_, complete, err = nativeTail(data, 3, 85)
	if err != nil || !complete {
		t.Fatalf("both files read completely: complete=%t err=%v", complete, err)
	}
}

func TestNativeTailPreservesIncompleteLastRecord(t *testing.T) {
	data := "==> stats.jsonl <==\n{\"ts\":99,\"viewer\":\"ok\",\"fps\":30,\"bitrate_bps\":3500000}\n{\"ts\":100"
	stats, complete, err := nativeTail(data, 100, 85)
	if err != nil || !complete {
		t.Fatal(complete, err)
	}
	c, _ := ParseCluster("")
	quality, err := NativeQuality(strings.NewReader(stats), 1, c, 85)
	if err != nil || !quality.Passed {
		t.Fatal(quality, err)
	}
}
