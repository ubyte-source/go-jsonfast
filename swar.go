package jsonfast

import (
	"math/bits"
	"unicode/utf8"
)

// wordSize is the number of bytes one SWAR word holds.
const wordSize = 8

// byteBits is the number of bits of one byte, the width of a SWAR lane.
const byteBits = 8

// SWAR words: every byte of each word holds the named value.
const (
	swarLo        uint64 = 0x0101010101010101
	swarHi               = swarLo << 7
	swarSpace            = swarLo * ' '
	swarQuote            = swarLo * '"'
	swarBackslash        = swarLo * '\\'
)

// borrowLanes sets the high bit of every zero byte of x; a borrow may also set
// it above a zero byte, so its high bits tell exactly whether x has one, and the
// lowest of them is exact. Its other bits are noise for the caller to mask off.
func borrowLanes(x uint64) uint64 {
	return (x - swarLo) &^ x
}

// plainByte reports whether JSON copies c as it is: ASCII from the space up,
// other than '"' and '\\'.
func plainByte(c byte) bool {
	return c >= ' ' && c < utf8.RuneSelf && c != '"' && c != '\\'
}

// plainMask flags the bytes of w that are not plain. A byte below ' ' borrows,
// which flags only bytes above one that is flagged already, so the lowest flag
// is exact.
func plainMask(w uint64) uint64 {
	return ((w - swarSpace) | w | borrowLanes(w^swarQuote) | borrowLanes(w^swarBackslash)) & swarHi
}

// bodyByte reports whether a string body holds c as it is, UTF-8 unchecked:
// any byte but a control byte, '"' and '\\'.
func bodyByte(c byte) bool {
	return c >= ' ' && c != '"' && c != '\\'
}

// bodyMask flags the bytes of w that are not body bytes; its lowest flag is exact.
func bodyMask(w uint64) uint64 {
	return ((w-swarSpace)&^w | borrowLanes(w^swarQuote) | borrowLanes(w^swarBackslash)) & swarHi
}

// firstLane returns the index in its word of the byte that the lowest set bit
// of m flags; load64 reads little-endian, so that byte comes first.
func firstLane(m uint64) int {
	return bits.TrailingZeros64(m) / byteBits
}

// plainRun returns the index of the first byte at or after data[j] that is not plain,
// or len(data): data[j] alone, then a word at a time to the last word of data; a tail
// under a word is that word shifted right past the bytes before it, zeros stopping it.
func plainRun(data []byte, j int) int {
	if j == len(data) || !plainByte(data[j]) {
		return j
	}
	j++
	last := len(data) - wordSize
	switch {
	case last < 0:
		return plainBytes(data, j)
	case j > last:
		return j + firstLane(plainMask(load64(data[last:], 0)>>(byteBits*(j-last))))
	}
	for ; j < last; j += wordSize {
		if m := plainMask(load64(data, j)); m != 0 {
			return j + firstLane(m)
		}
	}
	if m := plainMask(load64(data, last)); m != 0 {
		return last + firstLane(m)
	}
	return len(data)
}

// plainBytes is plainRun one byte at a time.
func plainBytes(data []byte, j int) int {
	for j < len(data) && plainByte(data[j]) {
		j++
	}
	return j
}

// bodyRun returns the index of the first byte at or after data[j] that is not
// a body byte, or len(data), testing one SWAR word at a time.
func bodyRun(data []byte, j int) int {
	for last := len(data) - wordSize; j <= last; j += wordSize {
		if m := bodyMask(load64(data, j)); m != 0 {
			return j + firstLane(m)
		}
	}
	return bodyBytes(data, j)
}

// bodyBytes is bodyRun one byte at a time.
func bodyBytes(data []byte, j int) int {
	for j < len(data) && bodyByte(data[j]) {
		j++
	}
	return j
}

// cleanRun returns the index of the first byte at or after data[j] that ends a
// string body or needs decoding: past plainRun, only valid multi-byte UTF-8 is
// clean, and it alone decodes to more than one byte.
func cleanRun(data []byte, j int) int {
	for {
		j = plainRun(data, j)
		if j == len(data) || !bodyByte(data[j]) {
			return j
		}
		_, size := utf8.DecodeRune(data[j:])
		if size == 1 {
			return j
		}
		j += size
	}
}
