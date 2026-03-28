# iters

Generic iterators library in Go.

Apart from providing a base interface for iterators:

```go
type Iter[T any] interface {
	// Next returns the next item.
	// It returns io.EOF if there are no more items left.
	Next() (T, error)
	Close()
}
```

It also provides a more typical iterator interface for integrating with various APIs:

```go
type PageIter[T any] interface {
	// NextPage returns the next page of items.
	// It returns io.EOF if there are no more items left.
	NextPage(ctx context.Context) ([]T, error)
	Close()
}
```

The library allows you to convert between these iterator interfaces,
and provides additional tooling like filtering, converting values, etc.

For Go 1.23+ conversion to/from `iter.Seq` is available.

One other useful feature is encoding/decoding iterators to/from JSON streams (or any other encoder/decoder).

## License

MIT