package iters

import (
	"context"
	"io"
)

// MultiIter creates a new iterator that alternates between results from sub iterators.
//
// If strict is set to true, the iterator will fail on the first non io.EOF error.
// If it's false, the iterator will return the last error when all iterators are done (or fail).
func MultiIter[T any](strict bool, sub ...Iter[T]) Iter[T] {
	return &multiIter[T]{sub: sub, ind: 0, strict: strict}
}

type multiIter[T any] struct {
	sub    []Iter[T]
	ind    int
	last   error
	strict bool
}

func (it *multiIter[T]) Next() (T, error) {
	var zero T
	for {
		if len(it.sub) == 0 {
			if it.last != nil {
				return zero, it.last
			}
			return zero, io.EOF
		}
		cur, err := it.sub[it.ind].Next()
		if err == nil {
			it.ind = (it.ind + 1) % len(it.sub)
			return cur, nil
		}
		it.sub[it.ind].Close()
		it.sub = append(it.sub[:it.ind], it.sub[it.ind+1:]...)
		if len(it.sub) != 0 {
			it.ind %= len(it.sub)
		}
		if err != io.EOF {
			if it.strict {
				return zero, err
			}
			it.last = err
		}
	}
}

func (it *multiIter[T]) Close() {
	for _, iter := range it.sub {
		iter.Close()
	}
	it.sub = nil
}

// MultiIterCtx creates a new iterator that alternates between results from sub iterators.
//
// If strict is set to true, the iterator will fail on the first non io.EOF error.
// If it's false, the iterator will return the last error when all iterators are done (or fail).
func MultiIterCtx[T any](strict bool, sub ...IterCtx[T]) IterCtx[T] {
	return &multiIterCtx[T]{sub: sub, ind: 0, strict: strict}
}

type multiIterCtx[T any] struct {
	sub    []IterCtx[T]
	ind    int
	last   error
	strict bool
}

func (it *multiIterCtx[T]) NextCtx(ctx context.Context) (T, error) {
	var zero T
	for {
		if len(it.sub) == 0 {
			if it.last != nil {
				return zero, it.last
			}
			return zero, io.EOF
		}
		cur, err := it.sub[it.ind].NextCtx(ctx)
		if err == nil {
			it.ind = (it.ind + 1) % len(it.sub)
			return cur, nil
		}
		it.sub[it.ind].Close()
		it.sub = append(it.sub[:it.ind], it.sub[it.ind+1:]...)
		if len(it.sub) != 0 {
			it.ind %= len(it.sub)
		}
		if err != io.EOF {
			if it.strict {
				return zero, err
			}
			it.last = err
		}
	}
}

func (it *multiIterCtx[T]) Close() {
	for _, iter := range it.sub {
		iter.Close()
	}
	it.sub = nil
}

// MultiPageIter creates a new iterator that alternates between pages from sub iterators.
//
// If strict is set to true, the iterator will fail on the first non io.EOF error.
// If it's false, the iterator will return the last error when all iterators are done (or fail).
func MultiPageIter[T any](strict bool, sub ...PageIter[T]) PageIter[T] {
	return &multiPageIter[T]{sub: sub, ind: 0, strict: strict}
}

type multiPageIter[T any] struct {
	sub    []PageIter[T]
	ind    int
	last   error
	strict bool
}

func (it *multiPageIter[T]) NextPage(ctx context.Context) ([]T, error) {
	for {
		if len(it.sub) == 0 {
			if it.last != nil {
				return nil, it.last
			}
			return nil, io.EOF
		}
		page, err := it.sub[it.ind].NextPage(ctx)
		if err == nil {
			it.ind = (it.ind + 1) % len(it.sub)
			return page, nil
		}
		it.sub[it.ind].Close()
		it.sub = append(it.sub[:it.ind], it.sub[it.ind+1:]...)
		if len(it.sub) != 0 {
			it.ind %= len(it.sub)
		}
		if err != io.EOF {
			if it.strict {
				return nil, err
			}
			it.last = err
		}
	}
}

func (it *multiPageIter[T]) Close() {
	for _, iter := range it.sub {
		iter.Close()
	}
	it.sub = nil
}
