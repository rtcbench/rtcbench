package bench

import (
	"context"
	"fmt"
)

type Attempt struct {
	Load   int  `json:"load"`
	Passed bool `json:"passed"`
}

type Bounds struct {
	Min  int `json:"min"`
	Max  int `json:"max"`
	Hint int `json:"hint,omitempty"`
}

type SearchResult struct {
	Complete       bool      `json:"complete"`
	Limit          int       `json:"limit"`
	LimitReason    string    `json:"limitReason"`
	Best           int       `json:"best"`
	CeilingReached bool      `json:"ceilingReached"`
	Attempts       []Attempt `json:"attempts"`
}

func Search(ctx context.Context, bounds Bounds, test func(context.Context, int) (bool, error)) (SearchResult, error) {
	result := SearchResult{Attempts: []Attempt{}, Limit: bounds.Max, LimitReason: "search bound"}
	if bounds.Min < 1 || bounds.Max < bounds.Min || bounds.Max > 1_000_000 {
		return result, fmt.Errorf("search bounds must satisfy 1 <= min <= max <= 1000000")
	}
	lo, hi := bounds.Min, bounds.Max
	for lo <= hi {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		n := lo + (hi-lo)/2
		if len(result.Attempts) == 0 && bounds.Hint >= lo && bounds.Hint <= hi {
			n = bounds.Hint
		}
		passed, err := test(ctx, n)
		if err != nil {
			return result, err
		}
		result.Attempts = append(result.Attempts, Attempt{Load: n, Passed: passed})
		if passed {
			result.Best = n
			lo = n + 1
		} else {
			hi = n - 1
		}
	}
	result.CeilingReached = result.Best == bounds.Max
	result.Complete = true
	return result, nil
}
