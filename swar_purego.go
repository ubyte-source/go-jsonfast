//go:build !((amd64 || arm64 || ppc64le || s390x) && !purego)

package jsonfast

// load64 returns the 8 bytes of b starting at j as a little-endian
// word. The byte-wise form is well-defined on every architecture
// (no alignment requirements) and the SWAR predicates are byte-order
// agnostic, so little-endian assembly of the word is correct
// everywhere.
func load64(b []byte, j int) uint64 {
	_ = b[j+7] // bounds hint
	return uint64(b[j]) | uint64(b[j+1])<<8 | uint64(b[j+2])<<16 |
		uint64(b[j+3])<<24 | uint64(b[j+4])<<32 | uint64(b[j+5])<<40 |
		uint64(b[j+6])<<48 | uint64(b[j+7])<<56
}

// load64String is load64 for string input.
func load64String(s string, j int) uint64 {
	_ = s[j+7] // bounds hint
	return uint64(s[j]) | uint64(s[j+1])<<8 | uint64(s[j+2])<<16 |
		uint64(s[j+3])<<24 | uint64(s[j+4])<<32 | uint64(s[j+5])<<40 |
		uint64(s[j+6])<<48 | uint64(s[j+7])<<56
}
