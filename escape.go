package jsonfast

import "unicode/utf8"

// AppendEscaped appends p with JSON escaping and no surrounding quotes: the
// short escapes for '"', '\\', \b, \f, \n, \r and \t, \u00XX for the other
// control bytes, and U+FFFD for each byte utf8.DecodeRune rejects.
func (b *Builder) AppendEscaped(p []byte) {
	b.buf = appendEscaped(b.buf, p, plainRun(p, 0))
}

// AppendEscapedString appends s with JSON escaping and no surrounding quotes: the
// short escapes for '"', '\\', \b, \f, \n, \r and \t, \u00XX for the other
// control bytes, and U+FFFD for each byte utf8.DecodeRune rejects.
func (b *Builder) AppendEscapedString(s string) {
	b.AppendEscaped(bytesOf(s))
}

// escapeStackSize is how many bytes of output EscapeString builds on the
// stack before its buffer grows.
const escapeStackSize = 512

// EscapeString returns s with the short JSON escapes, \u00XX for the other control
// bytes and U+FFFD for each byte utf8.DecodeRune rejects; a string that needs none
// comes back as it is, and any other costs one allocation up to 512 bytes of output.
func EscapeString(s string) string {
	p := bytesOf(s)
	i := cleanRun(p, 0)
	if i == len(p) {
		return s
	}
	var stack [escapeStackSize]byte
	return string(appendEscaped(stack[:0], p, i))
}

// appendEscaped appends p to dst with JSON escaping; the first i bytes of p
// need none. Past a plain run, a byte that no string body holds as it is takes
// an escape, and any other starts UTF-8.
func appendEscaped(dst, p []byte, i int) []byte {
	dst = append(dst, p[:i]...)
	for i < len(p) {
		if c := p[i]; !bodyByte(c) {
			dst = appendEscapedASCII(dst, c)
			i++
		} else {
			dst, i = appendUTF8(dst, p, i)
		}
		j := plainRun(p, i)
		dst = append(dst, p[i:j]...)
		i = j
	}
	return dst
}

// appendUTF8 appends U+FFFD for a byte at p[i] that utf8.DecodeRune rejects, or
// else copies the run of valid UTF-8 there, and returns the index past it.
func appendUTF8(dst, p []byte, i int) (out []byte, next int) {
	r, n := utf8.DecodeRune(p[i:])
	if n == 1 {
		return utf8.AppendRune(dst, r), i + 1
	}
	j := cleanRun(p, i+n)
	return append(dst, p[i:j]...), j
}

// lowerHex holds the digits of a \u00XX escape.
const lowerHex = "0123456789abcdef"

// appendEscapedASCII appends the escape of c, an ASCII byte that is not plain.
func appendEscapedASCII(dst []byte, c byte) []byte {
	switch c {
	case '"', '\\':
		return append(dst, '\\', c)
	case '\b':
		return append(dst, '\\', 'b')
	case '\f':
		return append(dst, '\\', 'f')
	case '\n':
		return append(dst, '\\', 'n')
	case '\r':
		return append(dst, '\\', 'r')
	case '\t':
		return append(dst, '\\', 't')
	}
	return append(dst, '\\', 'u', '0', '0', lowerHex[c/hexBase], lowerHex[c%hexBase])
}
