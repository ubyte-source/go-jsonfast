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

// zeroLanes sets the high bit of every zero byte of x. A borrow may also set
// it above a zero byte, so the result tells exactly whether x has one, and its
// lowest flag is exact.
func zeroLanes(x uint64) uint64 {
	return (x - swarLo) &^ x & swarHi
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
	return ((w-swarSpace)|w)&swarHi | zeroLanes(w^swarQuote) | zeroLanes(w^swarBackslash)
}

// plainWord reports whether every byte of w is plain.
func plainWord(w uint64) bool {
	return plainMask(w) == 0
}

// bodyByte reports whether a string body holds c as it is, UTF-8 unchecked:
// any byte but a control byte, '"' and '\\'.
func bodyByte(c byte) bool {
	return c >= ' ' && c != '"' && c != '\\'
}

// bodyMask flags the bytes of w that are not body bytes; its lowest flag is
// exact, as plainMask's is.
func bodyMask(w uint64) uint64 {
	return (w-swarSpace)&^w&swarHi | zeroLanes(w^swarQuote) | zeroLanes(w^swarBackslash)
}

// firstLane returns the index in its word of the byte that the lowest set bit
// of m flags; load64 reads little-endian, so that byte comes first.
func firstLane(m uint64) int {
	return bits.TrailingZeros64(m) / byteBits
}

// plainRun returns the index of the first byte at or after data[j] that is not plain,
// or len(data). It tests data[j] alone, so a run that stops at once costs no word, then
// the words past it; after one or more, the word that ends data covers a plain tail.
func plainRun(data []byte, j int) int {
	if j != len(data) {
		if !plainByte(data[j]) {
			return j
		}
		j++
	}
	words := (len(data) - j) / wordSize
	for range words {
		if m := plainMask(load64(data, j)); m != 0 {
			return j + firstLane(m)
		}
		j += wordSize
	}
	if words > 0 && plainWord(load64(data, len(data)-wordSize)) {
		return len(data)
	}
	return plainBytes(data, j)
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
	for range (len(data) - j) / wordSize {
		if m := bodyMask(load64(data, j)); m != 0 {
			return j + firstLane(m)
		}
		j += wordSize
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
