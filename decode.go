package jsonfast

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"unicode/utf16"
	"unicode/utf8"
)

// DecodeStringView decodes the JSON string token raw, quotes included, as
// encoding/json does: a surrogate pair joins, and a lone surrogate escape or a byte
// utf8.DecodeRune rejects becomes U+FFFD. Clean input comes back as a view of raw.
func DecodeStringView[T Text](raw T) (T, bool) {
	v, st := decodeToken(bytesOf(raw))
	if st == strMalformed {
		var zero T
		return zero, false
	}
	return textOf[T](v), true
}

// DecodeString decodes the JSON string raw into a Go string by the
// DecodeStringView rule; string input that needs no decoding is not copied.
func DecodeString[T Text](raw T) (string, bool) {
	v, st := decodeToken(bytesOf(raw))
	switch st {
	case strMalformed:
		return "", false
	case strView:
		return string(textOf[T](v)), true
	default:
		return textOf[string](v), true
	}
}

// arena is the buffer that the strings a call decodes are carved from. A carved
// string is never written again, so it stays valid for as long as the caller keeps
// it, unless the call rewinds the arena past it.
type arena struct {
	buf []byte
}

// arenaGrowth is how much each new chunk outgrows the last, so a call that
// decodes many strings takes few chunks.
const arenaGrowth = 2

// arenaRoom caps the room a walk gives a new chunk for the rest of its document.
const arenaRoom = 4 << 10

// strStatus is the outcome of decoding one string token.
type strStatus uint8

const (
	strMalformed strStatus = iota // no string by the rule the token is read by
	strView                       // nothing to decode: the value aliases the input
	strDecoded                    // decoded into the arena
)

// decodeToken decodes the string token that spans all of d.
func decodeToken(d []byte) ([]byte, strStatus) {
	var a arena
	v, end, st := tokenAt(&a, d, 0, len(d), anyBody)
	if end != len(d) {
		return nil, strMalformed
	}
	return v, st
}

// tokenAt decodes by rule the string token at data[i] in one pass and returns
// its value, the index past it and the outcome; a value that needs decoding is carved
// from a, whose new chunks hold at least the rest of data up to room bytes.
func tokenAt(a *arena, data []byte, i, room int, lowest rune) (value []byte, end int, st strStatus) {
	if i == len(data) || data[i] != '"' {
		return nil, 0, strMalformed
	}
	j := cleanRun(data, i+1)
	if j < len(data) && data[j] == '"' {
		return data[i+1 : j : j], j + 1, strView
	}
	v, end, st := decodeFrom(a, data, i+1, j, min(len(data)-i, room), lowest)
	return a.carve(v), end, st
}

// strictToken is tokenAt by the ValidUTF8 rule, for a walk: it fails as soon as it
// reads a byte or escape that holds no code point.
func (a *arena) strictToken(data []byte, i int) (value []byte, end int, ok bool) {
	value, end, st := tokenAt(a, data, i, arenaRoom, utf8Body)
	return value, end, st != strMalformed
}

// grow returns v, the string being decoded at the end of the chunk in use, with room
// for n more bytes; when the chunk lacks them, v moves with what it holds to a new
// chunk of len(v)+n bytes, twice the last or floor, whichever is most.
func (a *arena) grow(v []byte, n, floor int) []byte {
	if cap(v)-len(v) >= n {
		return v
	}
	a.buf = make([]byte, 0, max(len(v)+n, arenaGrowth*cap(a.buf), floor))
	return append(a.buf, v...)
}

// carve ends v, the string decoded at the end of the chunk in use, and returns it
// capped; the chunk never writes it again, as later strings go past it.
func (a *arena) carve(v []byte) []byte {
	a.buf = a.buf[:len(a.buf)+len(v)]
	return slices.Clip(v)
}

// keep copies v into the arena, in a new chunk of floor bytes or more when it needs
// one, and returns the copy.
func (a *arena) keep(v []byte, floor int) []byte {
	return a.carve(append(a.grow(a.buf[len(a.buf):], len(v), floor), v...))
}

// arenaMark is where the chunk in use of an arena ended when a walk took the mark;
// its capacity names the chunk, as each new chunk outgrows the last.
type arenaMark struct {
	capacity, length int
}

// mark returns where the strings carved next start.
func (a *arena) mark() arenaMark {
	return arenaMark{capacity: cap(a.buf), length: len(a.buf)}
}

// rewind gives back the room of the strings carved since m, which the caller has let
// go: the chunk m saw keeps what it held then, and a later one is reused whole.
func (a *arena) rewind(m arenaMark) {
	if cap(a.buf) == m.capacity {
		a.buf = a.buf[:m.length]
		return
	}
	a.buf = a.buf[:0]
}

// decodeFrom decodes by rule the body of a string from data[j], whose clean run
// ends at data[k], at the end of a's chunk in use, moving to new chunks of floor bytes
// or more when it needs them, and checks what ends each run before it writes.
func decodeFrom(a *arena, data []byte, j, k, floor int, lowest rune) (v []byte, end int, st strStatus) {
	v = a.buf[len(a.buf):]
	for {
		switch {
		case k == len(data):
			return nil, 0, strMalformed
		case data[k] == '"':
			return append(a.grow(v, k-j, floor), data[j:k]...), k + 1, strDecoded
		}
		if b := shortEscape(data, k); b != 0 {
			v = append(append(a.grow(v, k-j+1, floor), data[j:k]...), b)
			j = k + shortEscapeLen
			k = cleanRun(data, j)
			continue
		}
		r, n := runeAt(data, k)
		if r < lowest {
			return nil, 0, strMalformed
		}
		var enc [utf8.UTFMax]byte
		size := utf8.EncodeRune(enc[:], r)
		v = append(append(a.grow(v, k-j+size, floor), data[j:k]...), enc[:size]...)
		j = k + n
		k = cleanRun(data, j)
	}
}

// What runeAt returns for input with no code point of its own: badRune for what
// decodes to U+FFFD by replacement, which a literal U+FFFD or its escape never
// does, and utf8 writes as U+FFFD; malformedRune for what no string may hold.
const (
	badRune       rune = -1
	malformedRune rune = -2
)

// Byte lengths of the escape forms: a short escape, a \uXXXX escape, a
// surrogate pair of two such escapes, and the hex digits of one.
const (
	shortEscapeLen   = 2
	unicodeEscapeLen = len(`\u`) + hexEscapeDigits
	pairEscapeLen    = 2 * unicodeEscapeLen
	hexEscapeDigits  = 4
)

// runeAt decodes what a string scan stops at that is no quote or short escape: a \u
// escape, a surrogate pair of two, or a byte, malformedRune when no body holds it and
// badRune when utf8.DecodeRune rejects it, as for a lone surrogate half.
func runeAt(data []byte, i int) (r rune, n int) {
	hi, ok := unicodeEscape(data, i)
	switch {
	case !ok && bodyByte(data[i]):
		return badRune, 1
	case !ok:
		return malformedRune, 1
	case !utf16.IsSurrogate(hi):
		return hi, unicodeEscapeLen
	}
	if lo, ok := unicodeEscape(data, i+unicodeEscapeLen); ok {
		if r = utf16.DecodeRune(hi, lo); r != utf8.RuneError {
			return r, pairEscapeLen
		}
	}
	return badRune, unicodeEscapeLen
}

// shortEscape returns the byte that the two-byte escape at data[j] stands for,
// or 0 when none starts there: \u takes four hex digits, which runeAt reads.
func shortEscape(data []byte, j int) byte {
	if data[j] != '\\' || j+1 == len(data) || data[j+1] == 'u' {
		return 0
	}
	return unescapeByte(data[j+1])
}

// unescapeByte returns the byte that the two-byte escape with letter c stands
// for, or 0 when c starts none.
func unescapeByte(c byte) byte {
	switch c {
	case '"', '\\', '/':
		return c
	case 'b':
		return '\b'
	case 'f':
		return '\f'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	}
	return 0
}

// unicodeEscape parses the \uXXXX escape at data[j], j at most len(data).
func unicodeEscape(data []byte, j int) (rune, bool) {
	e := data[j:min(j+unicodeEscapeLen, len(data))]
	if len(e) != unicodeEscapeLen || e[0] != '\\' || e[1] != 'u' {
		return 0, false
	}
	return hex4(e[len(`\u`):])
}

// hexLetterValue is the value of the hex digits a and A.
const hexLetterValue = 0xa

// hexDigitBits is the width of the value of one hex digit.
const hexDigitBits = 4

// The words of hex4, one byte per digit: each byte of each constant holds the named
// value, and hexPairs the low byte of each half.
const (
	hexLanes  uint32 = 0x01010101
	hexTops          = hexLanes * utf8.RuneSelf
	hexCase          = hexLanes * ('a' - 'A')
	hexNibble        = hexLanes * (1<<hexDigitBits - 1)
	hexPairs  uint32 = 0x00ff00ff
)

// Added to a word of bytes below utf8.RuneSelf, each sets the high bit of every byte
// that is the named character or more: hexFrom0 '0', hexPast9 the one after '9',
// hexFromA 'a' and hexPastF the one after 'f'.
const (
	hexFrom0 = hexTops - hexLanes*'0'
	hexPast9 = hexTops - hexLanes*('9'+1)
	hexFromA = hexTops - hexLanes*'a'
	hexPastF = hexTops - hexLanes*('f'+1)
)

// letterBit is the bit that the letters a to f and A to F have and the digits lack.
const letterBit = 6

// hex4 parses digits, four hex digits, as one word: each byte is a digit from '0' to
// '9' or, lowered, a letter from 'a' to 'f', which has letterBit and is worth its low
// nibble plus 9; a byte from utf8.RuneSelf up is neither, whatever it carries.
func hex4(digits []byte) (rune, bool) {
	w := binary.BigEndian.Uint32(digits)
	digit := (w + hexFrom0) &^ (w + hexPast9)
	letter := ((w | hexCase) + hexFromA) &^ ((w | hexCase) + hexPastF)
	if (digit|letter)&hexTops != hexTops {
		return 0, false
	}
	v := w&hexNibble + (w>>letterBit&hexLanes)*(hexLetterValue-1)
	v = (v | v>>hexDigitBits) & hexPairs
	return rune((v | v>>byteBits) & math.MaxUint16), true
}

// EqualString reports whether raw is one JSON string whose decoded content
// equals s, without decoding it into memory. It compares by the
// DecodeStringView rule, so an s holding invalid UTF-8 never matches.
func EqualString[T Text](raw T, s string) bool {
	d := bytesOf(raw)
	if len(d) == 0 || d[0] != '"' {
		return false
	}
	stop, equal := matchedPrefix(d, 1, bytesOf(s))
	return equal && stop == len(d)-1
}

// matchedPrefix compares the body of a string token from d[i] with s; it stops at the
// closing quote or the end of d, past the first clean run or rune s does not hold
// next, or at a malformed escape or a control byte; equal is a match to the quote.
func matchedPrefix(d []byte, i int, s []byte) (stop int, equal bool) {
	j := 0
	for {
		k := cleanRun(d, i)
		if !bytes.HasPrefix(s[j:], d[i:k]) {
			return k, false
		}
		i, j = k, j+k-i
		switch {
		case i == len(d):
			return i, false
		case d[i] == '"':
			return i, j == len(s)
		}
		if b := shortEscape(d, i); b != 0 {
			if j == len(s) || s[j] != b {
				return i + shortEscapeLen, false
			}
			i, j = i+shortEscapeLen, j+1
			continue
		}
		r, n := runeAt(d, i)
		if r == malformedRune {
			return i, false
		}
		var enc [utf8.UTFMax]byte
		size := utf8.EncodeRune(enc[:], r)
		if !bytes.HasPrefix(s[j:], enc[:size]) {
			return i + n, false
		}
		i, j = i+n, j+size
	}
}
