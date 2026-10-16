package bench

import (
	"encoding/json"
	"fmt"
	"strings"
)

func nativeTail(data string, limit int, since float64) (string, bool, error) {
	var stats strings.Builder
	files, count := 0, 0
	first := 0.0
	complete := true
	finish := func() {
		if files > 0 && count >= limit && first >= since {
			complete = false
		}
	}
	for _, line := range strings.SplitAfter(data, "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "==> ") && strings.HasSuffix(text, " <==") {
			finish()
			files++
			count, first = 0, 0
			continue
		}
		if text == "" {
			continue
		}
		if files == 0 {
			return "", false, fmt.Errorf("native stats missing file header")
		}
		if count == 0 {
			var sample streamSample
			if json.Unmarshal([]byte(line), &sample) == nil {
				first = sample.Timestamp
			}
		}
		count++
		stats.WriteString(line)
	}
	finish()
	if files == 0 {
		return "", false, fmt.Errorf("native stats missing file header")
	}
	return stats.String(), complete, nil
}
