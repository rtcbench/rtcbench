package bench

import (
	"context"
	"errors"
	"testing"
)

func TestSearchFindsExactBoundaryWithAnyHint(t *testing.T) {
	for ceiling := 0; ceiling <= 65; ceiling++ {
		for _, hint := range []int{0, 1, 12, 34, 64, 100} {
			seen := map[int]bool{}
			result, err := Search(context.Background(), Bounds{Min: 1, Max: 64, Hint: hint}, func(ctx context.Context, n int) (bool, error) {
				if n < 1 || n > 64 || seen[n] {
					t.Fatalf("invalid or repeated probe %d", n)
				}
				seen[n] = true
				return n <= ceiling, nil
			})
			if err != nil || !result.Complete || result.Best != min(ceiling, 64) || result.CeilingReached != (ceiling >= 64) {
				t.Fatalf("ceiling=%d hint=%d: %+v %v", ceiling, hint, result, err)
			}
		}
	}
}

func TestSearchDoesNotTreatErrorsAsSaturation(t *testing.T) {
	want := errors.New("SSH unavailable")
	result, err := Search(context.Background(), Bounds{Min: 1, Max: 50}, func(context.Context, int) (bool, error) { return false, want })
	if !errors.Is(err, want) || result.Complete || len(result.Attempts) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Search(ctx, Bounds{Min: 1, Max: 50}, func(context.Context, int) (bool, error) { t.Fatal("called after cancellation"); return false, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
