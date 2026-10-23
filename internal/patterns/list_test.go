package patterns

import (
	"context"
	"errors"
	"testing"
)

func TestNextCursor(t *testing.T) {
	c := "c"
	if NextCursor(false, &c) != nil {
		t.Error("last page returned a cursor")
	}
	if got := NextCursor(true, &c); got != &c {
		t.Errorf("got %v", got)
	}
}

func TestPaginate(t *testing.T) {
	pages := [][]int{{1, 2}, {3, 4}, {5, 6}}
	pager := func(calls *int) func(*string) ([]int, *string, error) {
		return func(after *string) ([]int, *string, error) {
			i := *calls
			*calls++
			var next *string
			if i+1 < len(pages) {
				s := "c"
				next = &s
			}
			return pages[i], next, nil
		}
	}
	tests := []struct {
		name          string
		limit         int64
		stopAfter     int
		wantPush      int
		wantPageCalls int
	}{
		{"all", 0, 0, 6, 3},
		{"limit", 3, 0, 3, 2},
		{"push stops", 0, 1, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, pushes := 0, 0
			err := Paginate(context.Background(), tt.limit, pager(&calls), func(int) bool {
				pushes++
				return tt.stopAfter == 0 || pushes < tt.stopAfter
			})
			if err != nil || pushes != tt.wantPush || calls != tt.wantPageCalls {
				t.Fatalf("err=%v pushes=%d calls=%d", err, pushes, calls)
			}
		})
	}

	t.Run("error", func(t *testing.T) {
		boom := errors.New("boom")
		err := Paginate(context.Background(), 0, func(*string) ([]int, *string, error) { return nil, nil, boom }, func(int) bool { return true })
		if err != boom {
			t.Fatalf("got %v", err)
		}
	})
}
