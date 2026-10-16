package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

type Analysis struct {
	Best        map[string]int `json:"best"`
	Experiments int            `json:"experiments"`
	Failures    []Trial        `json:"failures"`
}

func Analyze(reader io.Reader) (Analysis, error) {
	result := Analysis{Best: map[string]int{}, Failures: []Trial{}}
	type repetitions struct {
		passed   bool
		required int
		seen     map[int]bool
	}
	passed := map[string]map[int]*repetitions{}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var entry struct {
			Trial
			R      int   `json:"r"`
			Result *bool `json:"result"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return result, fmt.Errorf("experiment %d: %w", result.Experiments+1, err)
		}
		t := entry.Trial
		if t.Load == 0 {
			t.Load = entry.R
		}
		if entry.Result != nil {
			t.Passed = *entry.Result
		}
		result.Experiments++
		if t.Reason == "SIMULATED" {
			continue
		}
		key := t.Key()
		if passed[key] == nil {
			passed[key] = map[int]*repetitions{}
		}
		history := passed[key][t.Load]
		if history == nil {
			history = &repetitions{passed: true, seen: map[int]bool{}}
			passed[key][t.Load] = history
		}
		history.passed = history.passed && t.Passed
		history.required = max(history.required, t.Repetitions)
		if t.Repeat >= 1 && t.Repeat <= t.Repetitions {
			history.seen[t.Repeat] = true
		}
		if !t.Passed {
			result.Failures = append(result.Failures, t)
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	for key, loads := range passed {
		result.Best[key] = 0
		for load, history := range loads {
			if history.passed && len(history.seen) >= history.required {
				result.Best[key] = max(result.Best[key], load)
			}
		}
	}
	return result, nil
}

func (a Analysis) Print(w io.Writer) error {
	var output strings.Builder
	keys := make([]string, 0, len(a.Best))
	for key := range a.Best {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	fmt.Fprintf(&output, "Experiments: %d\n\n%-25s %s\n", a.Experiments, "SFU/client", "Highest passing load")
	for _, key := range keys {
		fmt.Fprintf(&output, "%-25s %d\n", key, a.Best[key])
	}
	for _, t := range a.Failures {
		fmt.Fprintf(&output, "%s at %d: %s %s\n", t.Key(), t.Load, t.Reason, t.Error)
	}
	_, err := io.WriteString(w, output.String())
	return err
}
