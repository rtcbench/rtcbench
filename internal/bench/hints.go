package bench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

func readHints(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("hints: %w", err)
	}
	hints := map[string]int{}
	for key, value := range raw {
		if strings.HasPrefix(key, "_") {
			continue
		}
		var n int
		if err := json.Unmarshal(value, &n); err != nil {
			return nil, fmt.Errorf("hint %s: %w", key, err)
		}
		if n > 0 {
			hints[key] = n
		}
	}
	return hints, nil
}

func hintPrefix(mode string) string {
	if mode == "sender" {
		return "sender/"
	}
	return ""
}

func LoadHints(path, mode string) (map[string]int, error) {
	all, err := readHints(path)
	if err != nil {
		return nil, err
	}
	hints := map[string]int{}
	for _, sfu := range SFUs {
		for _, client := range Clients {
			key := Cell{SFU: sfu, Client: client}.Key()
			if n := all[hintPrefix(mode)+key]; n > 0 {
				hints[key] = n
			}
		}
	}
	return hints, nil
}

func SaveHints(path, mode string, report Report) error {
	if report.Simulated {
		return nil
	}
	hints, err := readHints(path)
	if err != nil {
		return err
	}
	for key, result := range report.Results {
		if result.Best > 0 {
			hints[hintPrefix(mode)+key] = result.Best
		}
	}
	if err := writeJSONFile(path+".tmp", hints); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
