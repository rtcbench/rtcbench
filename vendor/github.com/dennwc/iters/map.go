package iters

import "context"

// Map iterator items to a different type.
func Map[T1 any, T2 any](it Iter[T1], conv func(v T1) (T2, error)) Iter[T2] {
	return &mapIter[T1, T2]{it: it, conv: conv}
}

type mapIter[T1 any, T2 any] struct {
	it   Iter[T1]
	conv func(v T1) (T2, error)
}

func (it *mapIter[T1, T2]) Next() (T2, error) {
	var zero T2
	v, err := it.it.Next()
	if err != nil {
		return zero, err
	}
	return it.conv(v)
}

func (it *mapIter[T1, T2]) Close() {
	it.it.Close()
}

// MapCtx maps iterator items to a different type.
func MapCtx[T1 any, T2 any](it IterCtx[T1], conv func(ctx context.Context, v T1) (T2, error)) IterCtx[T2] {
	return &mapIterCtx[T1, T2]{it: it, conv: conv}
}

type mapIterCtx[T1 any, T2 any] struct {
	it   IterCtx[T1]
	conv func(ctx context.Context, v T1) (T2, error)
}

func (it *mapIterCtx[T1, T2]) NextCtx(ctx context.Context) (T2, error) {
	var zero T2
	v, err := it.it.NextCtx(ctx)
	if err != nil {
		return zero, err
	}
	return it.conv(ctx, v)
}

func (it *mapIterCtx[T1, T2]) Close() {
	it.it.Close()
}

// MapPage maps page iterator items to a different type.
func MapPage[T1 any, T2 any](it PageIter[T1], conv func(ctx context.Context, v T1) (T2, error)) PageIter[T2] {
	return &mapPageIter[T1, T2]{it: it, conv: conv}
}

type mapPageIter[T1 any, T2 any] struct {
	it   PageIter[T1]
	conv func(ctx context.Context, v T1) (T2, error)
}

func (it *mapPageIter[T1, T2]) NextPage(ctx context.Context) ([]T2, error) {
	page, err := it.it.NextPage(ctx)
	if err != nil {
		return nil, err
	}
	var out []T2
	if len(page) != 0 {
		out = make([]T2, 0, len(page))
	}
	for _, v := range page {
		v2, err := it.conv(ctx, v)
		if err != nil {
			return out, err
		}
		out = append(out, v2)
	}
	return out, nil
}

func (it *mapPageIter[T1, T2]) Close() {
	it.it.Close()
}
