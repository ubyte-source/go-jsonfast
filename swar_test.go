package jsonfast

import (
	"encoding/binary"
	"testing"
)

func TestSwarSpecialEscape_SafeASCII(t *testing.T) {
	w := binary.LittleEndian.Uint64([]byte("abcdefgh"))
	if swarSpecialEscape(w) != 0 {
		t.Error("expected 0 for safe ASCII")
	}
}

func TestSwarSpecialEscape_Quote(t *testing.T) {
	w := binary.LittleEndian.Uint64([]byte(`abcd"fgh`))
	if swarSpecialEscape(w) == 0 {
		t.Error("expected non-zero for embedded quote")
	}
}

func TestSwarSpecialEscape_Backslash(t *testing.T) {
	w := binary.LittleEndian.Uint64([]byte(`abc\efgh`))
	if swarSpecialEscape(w) == 0 {
		t.Error("expected non-zero for embedded backslash")
	}
}

func TestSwarSpecialEscape_ControlChar(t *testing.T) {
	buf := []byte("abcdefgh")
	buf[3] = 0x01
	w := binary.LittleEndian.Uint64(buf)
	if swarSpecialEscape(w) == 0 {
		t.Error("expected non-zero for control char")
	}
}

func TestSwarSpecialEscape_HighBit(t *testing.T) {
	buf := []byte("abcdefgh")
	buf[5] = 0x80
	w := binary.LittleEndian.Uint64(buf)
	if swarSpecialEscape(w) == 0 {
		t.Error("expected non-zero for high-bit byte")
	}
}

func TestSwarSpecialEscape_AllZeros(t *testing.T) {
	if swarSpecialEscape(0) == 0 {
		t.Error("expected non-zero for all-NUL word")
	}
}

func TestSwarSpecialSkip_SafeASCII(t *testing.T) {
	w := binary.LittleEndian.Uint64([]byte("abcdefgh"))
	if swarSpecialSkip(w) != 0 {
		t.Error("expected 0 for safe ASCII")
	}
}

func TestSwarSpecialSkip_Quote(t *testing.T) {
	w := binary.LittleEndian.Uint64([]byte(`abcd"fgh`))
	if swarSpecialSkip(w) == 0 {
		t.Error("expected non-zero for embedded quote")
	}
}

func TestSwarSpecialSkip_Backslash(t *testing.T) {
	w := binary.LittleEndian.Uint64([]byte(`abc\efgh`))
	if swarSpecialSkip(w) == 0 {
		t.Error("expected non-zero for embedded backslash")
	}
}

func TestSwarSpecialSkip_ControlChar(t *testing.T) {
	buf := []byte("abcdefgh")
	buf[3] = 0x01
	w := binary.LittleEndian.Uint64(buf)
	if swarSpecialSkip(w) == 0 {
		t.Error("expected non-zero for control char")
	}
}

func TestSwarSpecialSkip_HighBit_Allowed(t *testing.T) {
	buf := []byte("abcdefgh")
	buf[5] = 0x80
	w := binary.LittleEndian.Uint64(buf)
	if swarSpecialSkip(w) != 0 {
		t.Error("expected 0 for high-bit byte in skip mode")
	}
}

func TestSwarSpecialSkip_HighBit_AllHigh(t *testing.T) {
	w := uint64(0x8080808080808080)
	if swarSpecialSkip(w) != 0 {
		t.Error("expected 0 for all-0x80 word in skip mode")
	}
}

func TestSwarSpecialEscape_Space(t *testing.T) {
	w := binary.LittleEndian.Uint64([]byte("abc efgh"))
	if swarSpecialEscape(w) != 0 {
		t.Error("expected 0 for embedded space")
	}
}

func TestSwarSpecialEscape_Byte0x1F(t *testing.T) {
	buf := []byte("abcdefgh")
	buf[2] = 0x1F
	w := binary.LittleEndian.Uint64(buf)
	if swarSpecialEscape(w) == 0 {
		t.Error("expected non-zero for 0x1F (last control char)")
	}
}

func TestSwarSpecialSkip_Space(t *testing.T) {
	w := binary.LittleEndian.Uint64([]byte("abc efgh"))
	if swarSpecialSkip(w) != 0 {
		t.Error("expected 0 for embedded space")
	}
}

func TestSwarSpecialSkip_Byte0x1F(t *testing.T) {
	buf := []byte("abcdefgh")
	buf[2] = 0x1F
	w := binary.LittleEndian.Uint64(buf)
	if swarSpecialSkip(w) == 0 {
		t.Error("expected non-zero for 0x1F (last control char)")
	}
}

func TestSwarConstants(t *testing.T) {
	tests := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"swarLo", swarLo, 0x0101010101010101},
		{"swarHi", swarHi, 0x8080808080808080},
		{"swarControl", swarControl, 0x2020202020202020},
		{"swarQuote", swarQuote, 0x2222222222222222},
		{"swarBackslash", swarBackslash, 0x5C5C5C5C5C5C5C5C},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = 0x%016X, want 0x%016X", tt.name, tt.got, tt.want)
		}
	}
}

func TestSwarEscapeVsSkip_Difference(t *testing.T) {
	for b := byte(0x80); b != 0; b++ {
		var buf [8]byte
		for i := range buf {
			buf[i] = 'a'
		}
		buf[0] = b
		w := binary.LittleEndian.Uint64(buf[:])
		escape := swarSpecialEscape(w)
		skip := swarSpecialSkip(w)
		if escape == 0 {
			t.Errorf("swarSpecialEscape missed high-bit byte 0x%02X", b)
		}
		if skip != 0 {
			t.Errorf("swarSpecialSkip triggered on high-bit byte 0x%02X", b)
		}
	}
}

func TestSwarEscapeVsSkip_Agreement(t *testing.T) {
	for b := byte(0); b < 0x80; b++ {
		var buf [8]byte
		for i := range buf {
			buf[i] = 'a'
		}
		buf[0] = b
		w := binary.LittleEndian.Uint64(buf[:])
		escape := swarSpecialEscape(w)
		skip := swarSpecialSkip(w)
		if (escape != 0) != (skip != 0) {
			t.Errorf("disagreement on ASCII byte 0x%02X: escape=%d, skip=%d",
				b, escape, skip)
		}
	}
}

func BenchmarkSwarSpecialEscape(b *testing.B) {
	w := binary.LittleEndian.Uint64([]byte("abcdefgh"))
	for b.Loop() {
		swarSpecialEscape(w)
	}
}

func BenchmarkSwarSpecialSkip(b *testing.B) {
	w := binary.LittleEndian.Uint64([]byte("abcdefgh"))
	for b.Loop() {
		swarSpecialSkip(w)
	}
}
