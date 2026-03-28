package iters

import "context"

// Iter is a generic interface for the iterator.
//
// Example:
//
//	defer it.Close()
//	for {
//		v, err := it.Next()
//		if err == io.EOF {
//			break
//		} else if err != nil {
//			panic(err)
//		}
//		fmt.Println(v)
//	}
type Iter[T any] interface {
	// Next returns the next item.
	// It returns io.EOF if there are no more items left.
	Next() (T, error)
	// Close the iterator.
	Close()
}

// IterCtx is a generic interface for the iterator with a context.
//
// Example:
//
//	defer it.Close()
//	for {
//		v, err := it.NextCtx(ctx)
//		if err == io.EOF {
//			break
//		} else if err != nil {
//			panic(err)
//		}
//		fmt.Println(v)
//	}
type IterCtx[T any] interface {
	// NextCtx returns the next item.
	// It returns io.EOF if there are no more items left.
	NextCtx(ctx context.Context) (T, error)
	// Close the iterator.
	Close()
}
