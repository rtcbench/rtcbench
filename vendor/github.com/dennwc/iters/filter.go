package iters

import "context"

// Filter iterator results with a function. If function returns false, result is skipped.
func Filter[T any](it Iter[T], filter func(v T) bool) Iter[T] {
	if filter == nil {
		return it
	}
	return &filterIter[T]{it: it, filter: filter}
}

type filterIter[T any] struct {
	it     Iter[T]
	filter func(v T) bool
}

func (it *filterIter[T]) Next() (T, error) {
	var zero T
	for {
		v, err := it.it.Next()
		if err != nil {
			return zero, err
		}
		if it.filter(v) {
			return v, nil
		}
	}
}

func (it *filterIter[T]) Close() {
	it.it.Close()
}

// FilterCtx filters iterator results with a function. If function returns false, result is skipped.
func FilterCtx[T any](it IterCtx[T], filter func(v T) bool) IterCtx[T] {
	if filter == nil {
		return it
	}
	return &filterIterCtx[T]{it: it, filter: filter}
}

type filterIterCtx[T any] struct {
	it     IterCtx[T]
	filter func(v T) bool
}

func (it *filterIterCtx[T]) NextCtx(ctx context.Context) (T, error) {
	var zero T
	for {
		v, err := it.it.NextCtx(ctx)
		if err != nil {
			return zero, err
		}
		if it.filter(v) {
			return v, nil
		}
	}
}

func (it *filterIterCtx[T]) Close() {
	it.it.Close()
}

// FilterPage filters page iterator results with a function. If function returns false, result is skipped.
func FilterPage[T any](it PageIter[T], filter func(v T) bool) PageIter[T] {
	if filter == nil {
		return it
	}
	return &filterPageIter[T]{it: it, filter: filter}
}

type filterPageIter[T any] struct {
	it     PageIter[T]
	filter func(v T) bool
}

func (it *filterPageIter[T]) NextPage(ctx context.Context) ([]T, error) {
	for {
		page, err := it.it.NextPage(ctx)
		var out []T
		if len(page) != 0 {
			out = make([]T, 0, len(page))
		}
		for _, v := range page {
			if it.filter(v) {
				out = append(out, v)
			}
		}
		if err != nil {
			return out, err
		} else if len(out) != 0 {
			return out, nil
		}
	}
}

func (it *filterPageIter[T]) Close() {
	it.it.Close()
}
