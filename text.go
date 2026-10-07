package jsonfast

import "unsafe"

// Text is a JSON text in a string or a byte slice, named types included. Views of it
// have the caller's type and alias the input, capacity capped so an append copies, or
// a buffer nothing writes again; they last as long as the input and are read-only.
type Text interface{ ~string | ~[]byte }

// bytesOf returns the bytes of data without copying. The first two words of a
// slice header form a string header, so both shapes read the same way.
//
//nolint:gosec // reads the header prefix shared by both shapes; nothing writes
func bytesOf[T Text](data T) []byte {
	s := *(*string)(unsafe.Pointer(&data))
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// textOf returns b as a T without copying. A slice header begins with a string
// header, so the reinterpretation holds for both shapes.
//
//nolint:gosec // callers pass bytes that nothing writes again
func textOf[T Text](b []byte) T {
	return *(*T)(unsafe.Pointer(&b))
}

// stringOf returns data as a string without copying, for reads that end
// before the call returns.
func stringOf[T Text](data T) string {
	return textOf[string](bytesOf(data))
}

// view returns data[i:j] as a T whose capacity ends at j, so an append by the
// caller copies instead of writing over the input.
func view[T Text](data []byte, i, j int) T {
	return textOf[T](data[i:j:j])
}
