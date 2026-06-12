//go:build (amd64 || arm64 || ppc64le || s390x) && !purego

package jsonfast

import "unsafe"

// load64 returns the 8 bytes of b starting at j as a native-endian
// word. Callers guarantee 0 <= j && j+8 <= len(b). The architectures
// selected by the build constraint support unaligned 8-byte loads, and
// the SWAR predicates are byte-order agnostic, so a single raw load is
// both sound and exact here. Build with -tags=purego to force the
// portable implementation.
//
//nolint:gosec // bounds guaranteed by callers; arch supports unaligned loads
func load64(b []byte, j int) uint64 {
	return *(*uint64)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(b)), j))
}

// load64String is load64 for string input.
//
//nolint:gosec // bounds guaranteed by callers; arch supports unaligned loads
func load64String(s string, j int) uint64 {
	return *(*uint64)(unsafe.Add(unsafe.Pointer(unsafe.StringData(s)), j))
}
