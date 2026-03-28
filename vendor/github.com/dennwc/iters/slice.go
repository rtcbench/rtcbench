package iters

import (
	"context"
	"io"
)

// All returns all Iter results as a slice.
func All[T any](it Iter[T]) ([]T, error) {
	if s, ok := it.(*sliceIter[T]); ok {
		return s.buf, nil
	}
	if pi, ok := it.(PageIter[T]); ok {
		return AllPages(context.Background(), pi)
	}
	defer it.Close()
	var out []T
	for {
		v, err := it.Next()
		if err == io.EOF {
			return out, nil
		} else if err != nil {
			return out, err
		}
		out = append(out, v)
	}
}

// AllCtx returns all IterCtx results as a slice.
func AllCtx[T any](ctx context.Context, it IterCtx[T]) ([]T, error) {
	if s, ok := it.(*sliceIter[T]); ok {
		return s.buf, nil
	}
	if pi, ok := it.(PageIter[T]); ok {
		return AllPages(ctx, pi)
	}
	defer it.Close()
	var out []T
	for {
		v, err := it.NextCtx(ctx)
		if err == io.EOF {
			return out, nil
		} else if err != nil {
			return out, err
		}
		out = append(out, v)
	}
}

// AllPages returns all PageIter results as a slice.
func AllPages[T any](ctx context.Context, it PageIter[T]) ([]T, error) {
	if s, ok := it.(*sliceIter[T]); ok {
		return s.buf, nil
	}
	defer it.Close()
	var out []T
	for {
		v, err := it.NextPage(ctx)
		if err == io.EOF {
			return out, nil
		} else if err != nil {
			return out, err
		}
		out = append(out, v...)
	}
}

// Slice converts a slice to a PagedIter.
//
// When used as a PageIter, it returns all items as a single page.
// See PageSlice or IterWithPage for controlling the page size.
func Slice[T any](s []T) PagedIter[T] {
	return &sliceIter[T]{buf: s}
}

type sliceIter[T any] struct {
	buf []T
}

func (it *sliceIter[T]) NextPage(ctx context.Context) ([]T, error) {
	if len(it.buf) == 0 {
		return nil, io.EOF
	}
	buf := it.buf
	it.buf = nil
	return buf, nil
}

func (it *sliceIter[T]) Next() (T, error) {
	if len(it.buf) == 0 {
		var zero T
		return zero, io.EOF
	}
	cur := it.buf[0]
	it.buf = it.buf[1:]
	return cur, nil
}

func (it *sliceIter[T]) NextCtx(ctx context.Context) (T, error) {
	if len(it.buf) == 0 {
		var zero T
		return zero, io.EOF
	}
	cur := it.buf[0]
	it.buf = it.buf[1:]
	return cur, nil
}

func (it *sliceIter[T]) Close() {
	it.buf = nil
}

// PageSlice converts a slice of pages into a PageIter.
func PageSlice[T any](s [][]T) PageIter[T] {
	return &pageSliceIter[T]{pages: s}
}

type pageSliceIter[T any] struct {
	pages [][]T
}

func (it *pageSliceIter[T]) NextPage(ctx context.Context) ([]T, error) {
	if len(it.pages) == 0 {
		return nil, io.EOF
	}
	page := it.pages[0]
	it.pages = it.pages[1:]
	return page, nil
}

func (it *pageSliceIter[T]) Close() {
	it.pages = nil
}
