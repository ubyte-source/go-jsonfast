//go:build (amd64 || arm64 || ppc64le || s390x) && !purego

package jsonfast

import (
	"encoding/binary"
	"testing"
)

func TestLoad64ReadsNativeEndianWordsAtEveryOffset(t *testing.T) {
	buf := make([]byte, 64)
	for i := range buf {
		buf[i] = byte(i*7 + 13)
	}
	for j := 0; j+wordSize <= len(buf); j++ {
		if got, want := load64(buf, j), binary.NativeEndian.Uint64(buf[j:]); got != want {
			t.Fatalf("load64(buf, %d) = %#x, want %#x", j, got, want)
		}
	}
}
