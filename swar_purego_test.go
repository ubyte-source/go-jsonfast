//go:build !((amd64 || arm64 || ppc64le || s390x) && !purego)

package jsonfast

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestLoad64PutsByteJPlusKInLaneK(t *testing.T) {
	buf := make([]byte, wordSize*wordSize)
	for i := range buf {
		buf[i] = byte(i*wordSize - i)
	}
	for j := 0; j+wordSize <= len(buf); j++ {
		var lanes [wordSize]byte
		binary.LittleEndian.PutUint64(lanes[:], load64(buf, j))
		if !bytes.Equal(lanes[:], buf[j:j+wordSize]) {
			t.Fatalf("load64(buf, %d) has lanes %v, want %v", j, lanes, buf[j:j+wordSize])
		}
	}
}
