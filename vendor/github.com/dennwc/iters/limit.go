package iters

import "io"

// Limit the number of results returned by the iterator.
// It returns all results if limit < 0.
func Limit[T any](it Iter[T], limit int) Iter[T] {
	if limit < 0 {
		return it
	}
	return &limitIter[T]{it: it, limit: limit}
}

type limitIter[T any] struct {
	it    Iter[T]
	limit int
}

func (it *limitIter[T]) Next() (T, error) {
	if it.limit <= 0 {
		var zero T
		return zero, io.EOF
	}
	it.limit--
	return it.it.Next()
}

func (it *limitIter[T]) Close() {
	it.it.Close()
	it.limit = 0
}
