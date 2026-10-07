//go:build (amd64 || arm64 || ppc64le || s390x) && !purego

package jsonfast

import "unsafe"

// load64 returns the 8 bytes of b at j as a native-endian word in one
// unaligned load; callers keep j+8 <= len(b), and SWAR ignores byte order.
//
//nolint:gosec // callers bound j+8 by len(b); these targets allow unaligned loads
func load64(b []byte, j int) uint64 {
	return *(*uint64)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(b)), j))
}
