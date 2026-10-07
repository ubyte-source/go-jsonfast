//go:build !((amd64 || arm64 || ppc64le || s390x) && !purego)

package jsonfast

import "encoding/binary"

// load64 returns the 8 bytes of b at j as one word; the SWAR predicates
// ignore byte order.
func load64(b []byte, j int) uint64 {
	return binary.LittleEndian.Uint64(b[j:])
}
