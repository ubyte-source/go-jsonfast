package jsonfast

// SWAR broadcast constants: each byte of the uint64 equals the named byte.
const (
	swarLo         = uint64(0x0101010101010101)
	swarHi         = uint64(0x8080808080808080)
	swarControl    = uint64(0x2020202020202020) // 0x20 (space)
	swarQuote      = uint64(0x2222222222222222) // '"'
	swarBackslash  = uint64(0x5C5C5C5C5C5C5C5C) // '\\'
	swarBraceOpen  = uint64(0x7B7B7B7B7B7B7B7B) // '{'
	swarBraceClose = uint64(0x7D7D7D7D7D7D7D7D) // '}'
	swarBrackOpen  = uint64(0x5B5B5B5B5B5B5B5B) // '['
	swarBrackClose = uint64(0x5D5D5D5D5D5D5D5D) // ']'
)

// swarSpecialEscape returns non-zero if w contains any byte that
// requires JSON-escape handling: < 0x20, '"', '\\', or ≥ 0x80.
func swarSpecialEscape(w uint64) uint64 {
	hasControl := (w - swarControl) & ^w & swarHi
	xq := w ^ swarQuote
	xb := w ^ swarBackslash
	return hasControl | w&swarHi | (xq-swarLo)&^xq&swarHi | (xb-swarLo)&^xb&swarHi
}

// swarSpecialSkip returns non-zero if w contains < 0x20, '"', or '\\'.
// ≥ 0x80 is allowed (valid UTF-8 continuation inside a string).
func swarSpecialSkip(w uint64) uint64 {
	hasControl := (w - swarControl) & ^w & swarHi
	xq := w ^ swarQuote
	xb := w ^ swarBackslash
	return hasControl | (xq-swarLo)&^xq&swarHi | (xb-swarLo)&^xb&swarHi
}
