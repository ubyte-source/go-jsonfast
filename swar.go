package jsonfast

// SWAR (SIMD Within A Register) constants for byte-at-a-time scanning.
// Each constant broadcasts a single byte across all 8 positions of a uint64.
const (
	swarLo        = uint64(0x0101010101010101) // byte 0x01 broadcast
	swarHi        = uint64(0x8080808080808080) // high-bit mask
	swarControl   = uint64(0x2020202020202020) // space (0x20) broadcast
	swarQuote     = uint64(0x2222222222222222) // '"' broadcast
	swarBackslash = uint64(0x5C5C5C5C5C5C5C5C) // '\\' broadcast
)

// swarSpecialEscape returns non-zero if w contains any byte requiring
// JSON-escape handling: control chars (< 0x20), '"', '\\', or non-ASCII (≥ 0x80).
// The non-ASCII check (w&swarHi) is needed because these bytes require
// UTF-8 validation in the escape path.
func swarSpecialEscape(w uint64) uint64 {
	hasControl := (w - swarControl) & ^w & swarHi
	xq := w ^ swarQuote
	xb := w ^ swarBackslash
	return hasControl | w&swarHi | (xq-swarLo)&^xq&swarHi | (xb-swarLo)&^xb&swarHi
}

// swarSpecialSkip returns non-zero if w contains a control char (< 0x20),
// '"', or '\\'. Unlike swarSpecialEscape, bytes ≥ 0x80 are NOT flagged
// because multi-byte UTF-8 is valid inside JSON strings and needs no
// special handling when merely skipping over content.
func swarSpecialSkip(w uint64) uint64 {
	hasControl := (w - swarControl) & ^w & swarHi
	xq := w ^ swarQuote
	xb := w ^ swarBackslash
	return hasControl | (xq-swarLo)&^xq&swarHi | (xb-swarLo)&^xb&swarHi
}
