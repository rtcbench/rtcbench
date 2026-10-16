package bench

import (
	"fmt"
	"strings"
	"testing"
)

func TestNativeQualityIncludesMissingAndStaleStreams(t *testing.T) {
	c, _ := ParseCluster("")
	for _, tt := range []struct {
		name               string
		expected, reported int
		fps, ts            float64
		reason             string
	}{
		{"one", 1, 1, 20, 100, "OK"},
		{"twenty", 20, 20, 20, 100, "OK"},
		{"missing", 5, 4, 20, 100, "STREAMS_MISSING"},
		{"none", 5, 0, 20, 100, "NO_STREAMS"},
		{"slow", 5, 5, 19, 100, "QUALITY_DEGRADED"},
		{"stale", 5, 5, 30, 50, "NO_STREAMS"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var input strings.Builder
			for i := 0; i < tt.reported; i++ {
				fmt.Fprintf(&input, "{\"ts\":%g,\"viewer\":\"v%d\",\"fps\":%g,\"bitrate_bps\":2800000}\n", tt.ts, i, tt.fps)
			}
			q, err := NativeQuality(strings.NewReader(input.String()), tt.expected, c, 90)
			if err != nil || q.Reason != tt.reason || q.Passed != (tt.reason == "OK") {
				t.Fatalf("%+v %v", q, err)
			}
		})
	}
}

func TestNativeQualityToleratesOnlyIncompleteFinalRecords(t *testing.T) {
	c, _ := ParseCluster("")
	valid := "{\"ts\":100,\"viewer\":\"v\",\"fps\":30,\"bitrate_bps\":3500000}\n"
	for _, input := range []string{valid, valid + `{"ts":`} {
		q, err := NativeQuality(strings.NewReader(input), 1, c, 90)
		if err != nil || !q.Passed {
			t.Fatalf("%q: %+v %v", input, q, err)
		}
	}
	for _, input := range []string{valid + "broken\n", valid + "{\"ts\":\n"} {
		if _, err := NativeQuality(strings.NewReader(input), 1, c, 90); err == nil {
			t.Fatalf("accepted corrupt complete line %q", input)
		}
	}
}

func TestNativeQualityUsesNewestRecordAndExpectedDenominator(t *testing.T) {
	c, _ := ParseCluster("")
	c.HealthyFraction = .99
	input := `{"ts":101,"viewer":"v","fps":0,"bitrate_bps":2800000}
{"ts":100,"viewer":"v","fps":30,"bitrate_bps":3500000}`
	q, err := NativeQuality(strings.NewReader(input), 1, c, 90)
	if err != nil || q.Passed || q.Reported != 1 {
		t.Fatalf("%+v %v", q, err)
	}
}

func TestAnalysisRejectsLuckyPassAndSimulations(t *testing.T) {
	a, err := Analyze(strings.NewReader(`{"sfu":"livekit","client":"rtcbench","r":34,"result":true}
{"sfu":"livekit","client":"rtcbench","r":35,"result":true}
{"sfu":"livekit","client":"rtcbench","r":35,"result":false}
{"sfu":"mediasoup","client":"chromium","r":5,"result":true}
{"sfu":"livekit","client":"rtcbench","r":100,"result":true,"reason":"SIMULATED"}`))
	if err != nil || a.Best["livekit/rtcbench"] != 34 || a.Best["mediasoup/chromium"] != 5 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestAnalysisDoesNotPromoteAnIncompleteRepetitionSet(t *testing.T) {
	first := `{"sfu":"livekit","client":"rtcbench","load":5,"passed":true,"repeat":1,"repetitions":2}`
	a, err := Analyze(strings.NewReader(first))
	if err != nil || a.Best["livekit/rtcbench"] != 0 {
		t.Fatalf("incomplete trial promoted: %+v %v", a, err)
	}
	a, err = Analyze(strings.NewReader(first + "\n" + `{"sfu":"livekit","client":"rtcbench","load":5,"passed":true,"repeat":2,"repetitions":2}`))
	if err != nil || a.Best["livekit/rtcbench"] != 5 {
		t.Fatalf("completed repetitions: %+v %v", a, err)
	}
}
