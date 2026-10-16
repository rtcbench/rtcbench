package bench

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBrowserReceiverQualityCountsPagesAndUsesDecodedFrames(t *testing.T) {
	c, _ := ParseCluster("")
	header := "datetime,participantName,trackId,videoRecvBitrates,videoRecvFps,videoRecvFrameRate\n"
	for _, tt := range []struct {
		name, rows   string
		decode, pass bool
		reported     int
	}{
		{
			"padding tracks",
			"99000,p0,video,3500000,30,30\n99000,p0,padding,0,0,\n99000,p1,video,3500000,30,30\n",
			true,
			true,
			2,
		},
		{
			"duplicate tracks do not replace missing page",
			"99000,p0,a,3500000,30,30\n99000,p0,b,3500000,30,30\n",
			true,
			false,
			1,
		},
		{"stalled page", "99000,p0,video,3500000,30,30\n99000,p1,video,0,0,\n", true, false, 2},
		{
			"received is not decoded",
			"99000,p0,video,3500000,30,10\n99000,p1,video,3500000,30,30\n",
			true,
			false,
			2,
		},
		{
			"missing decoded rate",
			"99000,p0,video,3500000,30,\n99000,p1,video,3500000,30,30\n",
			true,
			false,
			2,
		},
		{
			"decode disabled",
			"99000,p0,video,3500000,0,\n99000,p1,video,3500000,0,\n",
			false,
			true,
			2,
		},
		{
			"stale page",
			"80000,p0,video,3500000,30,30\n99000,p1,video,3500000,30,30\n",
			true,
			false,
			1,
		},
		{
			"newer bad sample replaces good",
			"98000,p0,video,3500000,30,30\n99000,p0,video,0,0,0\n99000,p1,video,3500000,30,30\n",
			true,
			false,
			2,
		},
		{
			"one track must meet both thresholds",
			"99000,p0,a,3500000,30,10\n99000,p0,b,1000000,30,30\n99000,p1,video,3500000,30,30\n",
			true,
			false,
			2,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q, err := browserReceiverQuality(strings.NewReader(header+tt.rows), 2, c, tt.decode, 90)
			if err != nil || q.Passed != tt.pass || q.Reported != tt.reported {
				t.Fatalf("%+v %v", q, err)
			}
		})
	}
}

func TestBrowserReceiverQualityRejectsCorruptMetrics(t *testing.T) {
	c, _ := ParseCluster("")
	for _, row := range []string{
		"99000,p0,a,NaN,30",
		"99000,p0,a,3500000,-1",
		"NaN,p0,a,3500000,30",
		"99000,,a,3500000,30",
	} {
		_, err := browserReceiverQuality(strings.NewReader("datetime,participantName,trackId,videoRecvBitrates,videoRecvFrameRate\n"+row+"\n"), 1, c, true, 90)
		if err == nil {
			t.Fatalf("accepted %q", row)
		}
	}
}

func browserStatsReply(count int) string {
	var data strings.Builder
	fmt.Fprintf(&data, "%d\n", time.Now().Unix())
	data.WriteString("datetime,participantName,trackId,videoRecvBitrates,videoRecvFrameRate\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&data, "%d,p%d,video,3500000,30\n", time.Now().UnixMilli(), i)
	}
	return data.String()
}
