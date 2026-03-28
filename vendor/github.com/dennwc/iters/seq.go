//go:build go1.23

package iters

import (
	"context"
	"io"
	"iter"
)

// AsSeq returns a sequence of results. It stops if an error is encountered.
func AsSeq[T any](it Iter[T]) iter.Seq[T] {
	return func(yield func(T) bool) {
		defer it.Close()
		for {
			v, err := it.Next()
			if err != nil {
				return
			}
			if !yield(v) {
				return
			}
		}
	}
}

// AsSeqErr returns a sequence of results and non-EOF errors.
func AsSeqErr[T any](it Iter[T]) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		defer it.Close()
		for {
			v, err := it.Next()
			if err == io.EOF {
				return
			}
			if !yield(v, err) {
				return
			}
		}
	}
}

// Seq creates an iterator from a sequence.
func Seq[T any](it iter.Seq[T]) Iter[T] {
	next, stop := iter.Pull(it)
	return &seqIter[T]{next, stop}
}

type seqIter[T any] struct {
	next func() (T, bool)
	stop func()
}

func (it *seqIter[T]) Close() {
	it.stop()
}

func (it *seqIter[T]) Next() (T, error) {
	v, ok := it.next()
	if !ok {
		var zero T
		return zero, io.EOF
	}
	return v, nil
}

// SeqErr creates an iterator from a sequence of values and errors.
func SeqErr[T any](it iter.Seq2[T, error]) Iter[T] {
	next, stop := iter.Pull2(it)
	return &seqErrIter[T]{next, stop}
}

type seqErrIter[T any] struct {
	next func() (T, error, bool)
	stop func()
}

func (it *seqErrIter[T]) Close() {
	it.stop()
}

func (it *seqErrIter[T]) Next() (T, error) {
	v, err, ok := it.next()
	if !ok {
		var zero T
		return zero, io.EOF
	}
	return v, err
}

// SeqPages creates an iterator from a sequence of pages.
func SeqPages[T any](it iter.Seq[[]T]) PageIter[T] {
	next, stop := iter.Pull(it)
	return &seqPagesIter[T]{next, stop}
}

type seqPagesIter[T any] struct {
	next func() ([]T, bool)
	stop func()
}

func (it *seqPagesIter[T]) Close() {
	it.stop()
}

func (it *seqPagesIter[T]) NextPage(ctx context.Context) ([]T, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	v, ok := it.next()
	if !ok {
		return nil, io.EOF
	}
	return v, nil
}

// SeqErrPages creates an iterator from a sequence of pages and errors.
func SeqErrPages[T any](it iter.Seq2[[]T, error]) PageIter[T] {
	next, stop := iter.Pull2(it)
	return &seqErrPagesIter[T]{next, stop}
}

type seqErrPagesIter[T any] struct {
	next func() ([]T, error, bool)
	stop func()
}

func (it *seqErrPagesIter[T]) Close() {
	it.stop()
}

func (it *seqErrPagesIter[T]) NextPage(ctx context.Context) ([]T, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	v, err, ok := it.next()
	if !ok {
		return nil, io.EOF
	}
	return v, err
}
