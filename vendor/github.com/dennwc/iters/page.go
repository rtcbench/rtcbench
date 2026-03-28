package iters

import "context"

// PageIter is an iterator that iterates over pages of items.
type PageIter[T any] interface {
	// NextPage returns the next page of items.
	// It returns io.EOF if there are no more items left.
	NextPage(ctx context.Context) ([]T, error)
	// Close the iterator.
	Close()
}

// PagedIter combines Iter and PageIter.
type PagedIter[T any] interface {
	Iter[T]
	IterCtx[T]
	PageIter[T]
}

// PagesAsIter takes a PageIter and converts it to an Iter.
func PagesAsIter[T any](ctx context.Context, it PageIter[T]) Iter[T] {
	if it, ok := it.(Iter[T]); ok {
		return it
	}
	return &pageEachIter[T]{ctx: ctx, it: it}
}

type pageEachIter[T any] struct {
	ctx context.Context
	it  PageIter[T]
	buf []T
}

func (it *pageEachIter[T]) next() error {
	for len(it.buf) == 0 {
		buf, err := it.it.NextPage(it.ctx)
		if err != nil {
			return err
		}
		it.buf = buf
	}
	return nil
}

func (it *pageEachIter[T]) Next() (T, error) {
	if err := it.next(); err != nil {
		var zero T
		return zero, err
	}
	cur := it.buf[0]
	it.buf = it.buf[1:]
	return cur, nil
}

func (it *pageEachIter[T]) NextPage(ctx context.Context) ([]T, error) {
	if err := it.next(); err != nil {
		return nil, err
	}
	page := it.buf
	it.buf = nil
	return page, nil
}

func (it *pageEachIter[T]) Close() {
	it.it.Close()
}

// IterWithPage takes an Iter and converts it to an PageIter with a specific page size.
func IterWithPage[T any](it Iter[T], page int) PageIter[T] {
	if page <= 0 {
		panic("page size must be set")
	}
	return &pageIter[T]{it: it, buf: make([]T, 0, page)}
}

type pageIter[T any] struct {
	it  Iter[T]
	buf []T
	err error
}

func (it *pageIter[T]) NextPage(ctx context.Context) ([]T, error) {
	it.buf = it.buf[:0]
	if it.err != nil {
		return nil, it.err
	}
	for len(it.buf) < cap(it.buf) {
		cur, err := it.it.Next()
		if err != nil {
			if len(it.buf) != 0 {
				it.err = err
				break
			}
			return nil, err
		}
		it.buf = append(it.buf, cur)
	}
	return it.buf, nil
}

func (it *pageIter[T]) Close() {
	it.it.Close()
}

// PagesAsIterCtx takes a PageIter and converts it to an IterCtx.
func PagesAsIterCtx[T any](it PageIter[T]) IterCtx[T] {
	if it, ok := it.(IterCtx[T]); ok {
		return it
	}
	return &pageEachIterCtx[T]{it: it}
}

type pageEachIterCtx[T any] struct {
	it  PageIter[T]
	buf []T
}

func (it *pageEachIterCtx[T]) next(ctx context.Context) error {
	for len(it.buf) == 0 {
		buf, err := it.it.NextPage(ctx)
		if err != nil {
			return err
		}
		it.buf = buf
	}
	return nil
}

func (it *pageEachIterCtx[T]) NextCtx(ctx context.Context) (T, error) {
	if err := it.next(ctx); err != nil {
		var zero T
		return zero, err
	}
	cur := it.buf[0]
	it.buf = it.buf[1:]
	return cur, nil
}

func (it *pageEachIterCtx[T]) NextPage(ctx context.Context) ([]T, error) {
	if err := it.next(ctx); err != nil {
		return nil, err
	}
	page := it.buf
	it.buf = nil
	return page, nil
}

func (it *pageEachIterCtx[T]) Close() {
	it.it.Close()
}

// IterWithPageCtx takes an IterCtx and converts it to an PageIter with a specific page size.
func IterWithPageCtx[T any](it IterCtx[T], page int) PageIter[T] {
	if page <= 0 {
		panic("page size must be set")
	}
	return &pageIterCtx[T]{it: it, buf: make([]T, 0, page)}
}

type pageIterCtx[T any] struct {
	it  IterCtx[T]
	buf []T
	err error
}

func (it *pageIterCtx[T]) NextPage(ctx context.Context) ([]T, error) {
	it.buf = it.buf[:0]
	if it.err != nil {
		return nil, it.err
	}
	for len(it.buf) < cap(it.buf) {
		cur, err := it.it.NextCtx(ctx)
		if err != nil {
			if len(it.buf) != 0 {
				it.err = err
				break
			}
			return nil, err
		}
		it.buf = append(it.buf, cur)
	}
	return it.buf, nil
}

func (it *pageIterCtx[T]) Close() {
	it.it.Close()
}
