package iters

import (
	"context"
	"io"
)

// Join iterators by listing elements sequentially from each iterator in order.
// If any of the iterators fail, an error is returned.
func Join[T any](sub ...Iter[T]) Iter[T] {
	return &joinIter[T]{sub: sub}
}

type joinIter[T any] struct {
	sub []Iter[T]
}

func (it *joinIter[T]) Next() (T, error) {
	var zero T
	for {
		if len(it.sub) == 0 {
			return zero, io.EOF
		}
		v, err := it.sub[0].Next()
		if err == nil {
			return v, nil
		}
		it.sub[0].Close()
		if err != io.EOF {
			return zero, err
		}
		it.sub = it.sub[1:]
	}
}

func (it *joinIter[T]) Close() {
	for _, iter := range it.sub {
		iter.Close()
	}
	it.sub = nil
}

// JoinCtx iterators by listing elements sequentially from each iterator in order.
// If any of the iterators fail, an error is returned.
func JoinCtx[T any](sub ...IterCtx[T]) IterCtx[T] {
	return &joinIterCtx[T]{sub: sub}
}

type joinIterCtx[T any] struct {
	sub []IterCtx[T]
}

func (it *joinIterCtx[T]) NextCtx(ctx context.Context) (T, error) {
	var zero T
	for {
		if len(it.sub) == 0 {
			return zero, io.EOF
		}
		v, err := it.sub[0].NextCtx(ctx)
		if err == nil {
			return v, nil
		}
		it.sub[0].Close()
		if err != io.EOF {
			return zero, err
		}
		it.sub = it.sub[1:]
	}
}

func (it *joinIterCtx[T]) Close() {
	for _, iter := range it.sub {
		iter.Close()
	}
	it.sub = nil
}

// JoinPages joins iterators by listing pages sequentially from each iterator in order.
// If any of the iterators fail, an error is returned.
func JoinPages[T any](sub ...PageIter[T]) PageIter[T] {
	return &joinPageIter[T]{sub: sub}
}

type joinPageIter[T any] struct {
	sub []PageIter[T]
}

func (it *joinPageIter[T]) NextPage(ctx context.Context) ([]T, error) {
	for {
		if len(it.sub) == 0 {
			return nil, io.EOF
		}
		v, err := it.sub[0].NextPage(ctx)
		if err == nil {
			return v, nil
		}
		it.sub[0].Close()
		if err != io.EOF {
			return nil, err
		}
		it.sub = it.sub[1:]
	}
}

func (it *joinPageIter[T]) Close() {
	for _, iter := range it.sub {
		iter.Close()
	}
	it.sub = nil
}
