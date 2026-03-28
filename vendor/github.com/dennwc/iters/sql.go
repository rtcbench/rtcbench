package iters

import "io"

// RowsScanner is a minimal interface that sql.Rows implements.
type RowsScanner interface {
	Next() bool
	Err() error
	Close() error
}

// FromRows converts sql.Rows to Iter.
func FromRows[T any](rows RowsScanner, scan func() (T, error)) Iter[T] {
	return &rowsIter[T]{
		rows: rows,
		scan: scan,
	}
}

type rowsIter[T any] struct {
	rows RowsScanner
	scan func() (T, error)
}

func (it *rowsIter[T]) Close() {
	_ = it.rows.Close()
}
func (it *rowsIter[T]) Next() (T, error) {
	var zero T
	if !it.rows.Next() {
		err := it.rows.Err()
		if err == nil {
			err = io.EOF
		}
		return zero, err
	}
	return it.scan()
}
