//go:build !((amd64 || arm64 || ppc64le) && !purego)

package jsonfast

import "encoding/binary"

// load64 returns the 8 bytes of b at j as a little-endian word.
func load64(b []byte, j int) uint64 {
	return binary.LittleEndian.Uint64(b[j:])
}
