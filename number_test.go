package jsonfast

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"
)

// boundaryIntegers returns every uint64 up to MaxUint16, every power of ten a
// uint64 holds with its neighbors, and the extremes.
func boundaryIntegers() []uint64 {
	var out []uint64
	for v := range uint64(math.MaxUint16 + 1) {
		out = append(out, v)
	}
	for p := uint64(decimalRadix); p <= math.MaxUint64/decimalRadix; p *= decimalRadix {
		out = append(out, p-1, p, p+1)
	}
	return append(out, math.MaxInt64, -math.MinInt64, math.MaxUint64-1, math.MaxUint64)
}

func TestBuilderAppendUint64MatchesStrconv(t *testing.T) {
	b := New(0)
	for _, v := range boundaryIntegers() {
		b.Reset()
		b.appendUint64(v)
		if got, want := string(b.Bytes()), strconv.FormatUint(v, decimalRadix); got != want {
			t.Errorf("appendUint64(%d) = %s, want %s", v, got, want)
		}
	}
}

func TestBuilderAppendInt64MatchesStrconv(t *testing.T) {
	b := New(0)
	for _, u := range boundaryIntegers() {
		for _, v := range []int64{int64(u & math.MaxInt64), -int64(u & math.MaxInt64), math.MinInt64} {
			b.Reset()
			b.appendInt64(v)
			if got, want := string(b.Bytes()), strconv.FormatInt(v, decimalRadix); got != want {
				t.Errorf("appendInt64(%d) = %s, want %s", v, got, want)
			}
		}
	}
}

func FuzzBuilderAppendInt64(f *testing.F) {
	for _, u := range boundaryIntegers()[math.MaxUint16:] {
		f.Add(int64(u&math.MaxInt64), u)
	}
	f.Fuzz(func(t *testing.T, v int64, u uint64) {
		b := New(0)
		b.appendInt64(v)
		if got, want := string(b.Bytes()), strconv.FormatInt(v, decimalRadix); got != want {
			t.Fatalf("appendInt64(%d) = %s, want %s", v, got, want)
		}
		strictlyRead(t, b.Bytes())
		b.Reset()
		b.appendUint64(u)
		if got, want := string(b.Bytes()), strconv.FormatUint(u, decimalRadix); got != want {
			t.Fatalf("appendUint64(%d) = %s, want %s", u, got, want)
		}
		strictlyRead(t, b.Bytes())
	})
}

// float64Oracle returns what encoding/json writes for v, and null for NaN and
// ±Inf, which it refuses.
func float64Oracle(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return litNull
	}
	out, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(out)
}

// boundaryFloats returns every power of two and of ten in float64 range and the
// limit of the integer writer, with their neighbors, each also negated, and the
// special values.
func boundaryFloats() []float64 {
	out := []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat64, -math.MaxFloat64}
	add := func(v float64) {
		for _, x := range []float64{v, math.Nextafter(v, 0), math.Nextafter(v, math.Inf(1))} {
			out = append(out, x, -x)
		}
	}
	for _, radix := range []float64{2, decimalRadix} {
		for v := 1.0; !math.IsInf(v, 0); v *= radix {
			add(v)
		}
		for v := 1.0; v > 0; v /= radix {
			add(v)
		}
	}
	add(shortestIntLimit)
	return append(out, 0, math.Copysign(0, -1), math.Pi, math.E, 1/math.Pi)
}

func TestBuilderAppendFloat64MatchesEncodingJSON(t *testing.T) {
	b := New(0)
	for _, v := range boundaryFloats() {
		b.Reset()
		b.appendFloat64(v)
		if got, want := string(b.Bytes()), float64Oracle(v); got != want {
			t.Errorf("appendFloat64(%g) = %s, want %s", v, got, want)
		}
	}
}

func FuzzBuilderAppendFloat64(f *testing.F) {
	for _, v := range []float64{0, math.Copysign(0, -1), math.Pi, math.MaxFloat64, math.SmallestNonzeroFloat64} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, v float64) {
		b := New(0)
		b.appendFloat64(v)
		if got, want := string(b.Bytes()), float64Oracle(v); got != want {
			t.Fatalf("appendFloat64(%g) = %s, want %s", v, got, want)
		}
		strictlyRead(t, b.Bytes())
		if whole := math.Trunc(v); !math.IsInf(v, 0) {
			b.Reset()
			b.appendFloat64(whole)
			if got, want := string(b.Bytes()), float64Oracle(whole); got != want {
				t.Fatalf("appendFloat64(%g) = %s, want %s", whole, got, want)
			}
		}
	})
}

// Values the number benchmarks write: an integer of one digit, one of three
// and a Unix time in seconds.
const (
	oneDigit    = 7
	threeDigits = 512
	unixSeconds = 1705321845
)

func TestBuilderAppendInt64Allocs(t *testing.T) {
	b := New(0)
	assertAllocs(t, 0, func() {
		b.Reset()
		b.appendInt64(math.MinInt64)
		b.appendInt64(math.MaxInt64)
		b.appendUint64(math.MaxUint64)
	})
}

func TestBuilderAppendFloat64Allocs(t *testing.T) {
	b := New(0)
	assertAllocs(t, 0, func() {
		b.Reset()
		b.appendFloat64(math.SmallestNonzeroFloat32)
		b.appendFloat64(unixSeconds)
		b.appendFloat64(math.Pi)
		b.appendFloat64(math.NaN())
	})
}

// benchmarkInts runs write on a Builder for integers of each width.
func benchmarkInts(b *testing.B, write func(builder *Builder, v int64)) {
	b.Helper()
	for _, v := range []int64{oneDigit, threeDigits, unixSeconds, math.MinInt64} {
		text := strconv.FormatInt(v, decimalRadix)
		b.Run(text, func(b *testing.B) {
			benchWrite(b, text, func(builder *Builder) { write(builder, v) })
		})
	}
}

func BenchmarkBuilderAppendInt64(b *testing.B) {
	benchmarkInts(b, (*Builder).appendInt64)
}

func BenchmarkBuilderAppendInt64Strconv(b *testing.B) {
	benchmarkInts(b, func(builder *Builder, v int64) {
		builder.buf = strconv.AppendInt(builder.buf, v, decimalRadix)
	})
}

func BenchmarkBuilderAppendFloat64(b *testing.B) {
	for _, bc := range []struct {
		name string
		v    float64
	}{
		{"Integral", unixSeconds},
		{"Fraction", math.Pi},
		{"Exponent", math.SmallestNonzeroFloat32},
	} {
		b.Run(bc.name, func(b *testing.B) {
			want, err := json.Marshal(bc.v)
			noError(b, err)
			benchWrite(b, string(want), func(builder *Builder) { builder.appendFloat64(bc.v) })
		})
	}
}
