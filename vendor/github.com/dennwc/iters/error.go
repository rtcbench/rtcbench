package iters

import "context"

// Error creates Iter that always return an error.
func Error[T any](err error) PagedIter[T] {
	return errorIter[T]{err: err}
}

// ErrorCtx creates IterCtx that always return an error.
func ErrorCtx[T any](err error) PagedIter[T] {
	return errorIter[T]{err: err}
}

type errorIter[T any] struct {
	err error
}

func (errorIter[T]) Close() {}
func (it errorIter[T]) Next() (T, error) {
	var zero T
	return zero, it.err
}
func (it errorIter[T]) NextCtx(ctx context.Context) (T, error) {
	var zero T
	return zero, it.err
}
func (it errorIter[T]) NextPage(ctx context.Context) ([]T, error) {
	return nil, it.err
}
