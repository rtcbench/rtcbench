package iters

import (
	"io"
)

type Encoder interface {
	Encode(v any) error
}

type Decoder interface {
	Decode(v any) error
}

// Encode an iterator.
func Encode[T any](enc Encoder, it Iter[T]) error {
	defer it.Close()
	for {
		cur, err := it.Next()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if err = enc.Encode(cur); err != nil {
			return err
		}
	}
}

// Decode an iterator.
//
// It will use type T as a destination type, thus it must not be an interface.
func Decode[T any](dec Decoder) Iter[T] {
	return &decoder[T]{dec: dec}
}

type decoder[T any] struct {
	dec Decoder
}

func (it *decoder[T]) Next() (T, error) {
	var zero T
	if it.dec == nil {
		return zero, io.EOF
	}
	var cur T
	err := it.dec.Decode(&cur)
	if err == io.EOF {
		return zero, io.EOF // do it explicitly
	} else if err != nil {
		return zero, err
	}
	return cur, nil
}

func (it *decoder[T]) Close() {
	it.dec = nil
}
