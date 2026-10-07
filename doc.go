// Package jsonfast is a zero-allocation JSON builder and scanner, generic over
// Text, so callers pass a string, a byte slice or a named type such as
// json.RawMessage. The Builder writes JSON, the value writers the Go values
// DecodeValue builds, and escapes every name and every string value it takes
// as a Go string or, in the StringBytes writers, as a byte slice; the writers
// named Raw copy their input as it is. The scanners walk JSON where it lies
// and hand back views of the input; ValidUTF8, WalkStrings, WalkTokens,
// IterateObject and IterateDocument check a whole document once, under a depth
// bound, where input enters a program; and the decoders read strings, numbers
// and, with DecodeValue, whole documents by the rules of encoding/json; a
// string they decode from string input that needs no decoding aliases that
// input. Iterate*, Walk*, DecodeValue and FlattenObject return ErrMalformed,
// or ErrDuplicateName for a repeated member name, and a callback's first error
// unchanged, and their callbacks may see what precedes a fault; ValidUTF8,
// IsNumber, Find* and the other Decode* functions report success with a bool.
package jsonfast
