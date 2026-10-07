package jsonfast

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

// isPlainSpec states the plain bytes by their range.
func isPlainSpec(c byte) bool {
	return c >= '\x20' && c <= '\x7f' && c != '\x22' && c != '\x5c'
}

// isBodySpec states the body bytes by their range.
func isBodySpec(c byte) bool {
	return c >= '\x20' && c != '\x22' && c != '\x5c'
}

// laneWord returns a word of plain 'a' bytes whose given lanes hold c.
func laneWord(c byte, lanes ...int) uint64 {
	buf := []byte("aaaaaaaa")
	for _, lane := range lanes {
		buf[lane] = c
	}
	return binary.LittleEndian.Uint64(buf)
}

func TestPlainByteAndBodyByteClassifyEveryByte(t *testing.T) {
	for n := range math.MaxUint8 + 1 {
		c := byte(n)
		if got, want := plainByte(c), isPlainSpec(c); got != want {
			t.Errorf("plainByte(%#02x) = %v, want %v", c, got, want)
		}
		if got, want := bodyByte(c), isBodySpec(c); got != want {
			t.Errorf("bodyByte(%#02x) = %v, want %v", c, got, want)
		}
	}
}

func TestPlainWordAndBodyWordMatchTheirBytesInEveryLane(t *testing.T) {
	for lane := range wordSize {
		for n := range math.MaxUint8 + 1 {
			c := byte(n)
			w := laneWord(c, lane)
			if got, want := plainWord(w), isPlainSpec(c); got != want {
				t.Fatalf("plainWord(%#02x in lane %d) = %v, want %v", c, lane, got, want)
			}
			if got, want := bodyWord(w), isBodySpec(c); got != want {
				t.Fatalf("bodyWord(%#02x in lane %d) = %v, want %v", c, lane, got, want)
			}
		}
	}
}

func TestPlainWordAndBodyWordIgnoreBorrowsAcrossLanes(t *testing.T) {
	for _, lanes := range [][2]int{
		{0, 1}, {wordSize/2 - 1, wordSize / 2}, {wordSize - 2, wordSize - 1}, {0, wordSize - 1},
	} {
		for n := range math.MaxUint16 + 1 {
			lo, hi := byte(n), byte(n>>8)
			buf := []byte("aaaaaaaa")
			buf[lanes[0]], buf[lanes[1]] = lo, hi
			w := binary.LittleEndian.Uint64(buf)
			if got, want := plainWord(w), isPlainSpec(lo) && isPlainSpec(hi); got != want {
				t.Fatalf("plainWord(%#02x, %#02x in lanes %v) = %v, want %v", lo, hi, lanes, got, want)
			}
			if got, want := bodyWord(w), isBodySpec(lo) && isBodySpec(hi); got != want {
				t.Fatalf("bodyWord(%#02x, %#02x in lanes %v) = %v, want %v", lo, hi, lanes, got, want)
			}
		}
	}
}

func TestZeroLanesFindsAnyZeroByte(t *testing.T) {
	for lane := range wordSize {
		if zeroLanes(laneWord(0, lane)) == 0 {
			t.Errorf("zeroLanes(%#x) = 0, want the bit of the zero byte in lane %d", laneWord(0, lane), lane)
		}
	}
	for _, w := range []uint64{laneWord('a'), swarHi, ^uint64(0), swarLo} {
		if zeroLanes(w) != 0 {
			t.Errorf("zeroLanes(%#x) = %#x, want 0 for a word with no zero byte", w, zeroLanes(w))
		}
	}
	if zeroLanes(laneWord(0, 2, wordSize-1))&swarHi == 0 {
		t.Errorf("zeroLanes(%#x) = no high bit, want the bits of its two zero bytes", laneWord(0, 2, wordSize-1))
	}
}

func TestPlainRunAndBodyRunStopAtTheFirstByteTheyReject(t *testing.T) {
	for _, data := range runInputs("\x00\x1f\"\\\x80\xff") {
		for start := 0; start <= len(data); start++ {
			if got, want := plainRun(data, start), firstMatch(data, start, isPlainSpec); got != want {
				t.Fatalf("plainRun(%q, %d) = %d, want %d", data, start, got, want)
			}
			if got, want := bodyRun(data, start), firstMatch(data, start, isBodySpec); got != want {
				t.Fatalf("bodyRun(%q, %d) = %d, want %d", data, start, got, want)
			}
		}
	}
}

// runRule is what the shared form of plainRun and bodyRun asks of the bytes
// it keeps: a test of one word and a test of one byte.
type runRule interface {
	word(w uint64) bool
	single(c byte) bool
}

type plainRule struct{}

func (plainRule) word(w uint64) bool { return plainWord(w) }

func (plainRule) single(c byte) bool { return plainByte(c) }

type bodyRule struct{}

func (bodyRule) word(w uint64) bool { return bodyWord(w) }

func (bodyRule) single(c byte) bool { return bodyByte(c) }

// sharedRun is the one generic form that could replace plainRun and bodyRun.
func sharedRun[R runRule](data []byte, j int) int {
	var rule R
	for range (len(data) - j) / wordSize {
		if !rule.word(load64(data, j)) {
			break
		}
		j += wordSize
	}
	for j < len(data) && rule.single(data[j]) {
		j++
	}
	return j
}

// runUnits is how many copies of its unit a run benchmark scans.
const runUnits = 32

// benchmarkRun times run over runUnits copies of unit and a closing quote.
func benchmarkRun(b *testing.B, unit string, run func(data []byte, j int) int) {
	b.Helper()
	data := []byte(strings.Repeat(unit, runUnits) + quote)
	if got, want := run(data, 0), bytes.IndexByte(data, '"'); got != want {
		b.Fatalf("the run over %q stopped at %d, want the quote at %d", unit, got, want)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_ = run(data, 0)
	}
}

func BenchmarkPlainRun(b *testing.B) {
	benchmarkRun(b, "abcdefgh", plainRun)
}

func BenchmarkPlainRunSharedForm(b *testing.B) {
	benchmarkRun(b, "abcdefgh", sharedRun[plainRule])
}

func BenchmarkBodyRun(b *testing.B) {
	benchmarkRun(b, "abcdéfgh", bodyRun)
}

func BenchmarkBodyRunSharedForm(b *testing.B) {
	benchmarkRun(b, "abcdéfgh", sharedRun[bodyRule])
}
