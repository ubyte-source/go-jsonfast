package jsonfast

import (
	"math"
	"strconv"
)

// digitPairs holds the two-digit forms of 0 to 99, back to back.
const digitPairs = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

// Powers of ten that bound the numbers of one, two and three digits.
const (
	decimalBase = 10
	pairBase    = 100
	tripleBase  = 1000
)

// pairLen is the length of one entry of digitPairs.
const pairLen = 2

// appendInt64 writes v in decimal: for a negative v a minus and its magnitude,
// ^v+1, whose ^v has no sign bit, so math.MinInt64 has one too.
func (b *Builder) appendInt64(v int64) {
	if v < 0 {
		b.buf = append(b.buf, '-')
		b.appendUint64(uint64(^v&math.MaxInt64) + 1)
		return
	}
	b.appendUint64(uint64(v))
}

// appendUint64 writes v in decimal, two digits per division.
func (b *Builder) appendUint64(v uint64) {
	switch {
	case v < decimalBase:
		b.buf = append(b.buf, byte('0'+v))
		return
	case v < pairBase:
		tens, units := pair(v)
		b.buf = append(b.buf, tens, units)
		return
	case v < tripleBase:
		tens, units := pair(v % pairBase)
		b.buf = append(b.buf, byte('0'+v/pairBase), tens, units)
		return
	}
	var digits [len("18446744073709551615")]byte
	i := len(digits)
	for v >= pairBase {
		i -= pairLen
		digits[i], digits[i+1] = pair(v % pairBase)
		v /= pairBase
	}
	if v >= decimalBase {
		i -= pairLen
		digits[i], digits[i+1] = pair(v)
	} else {
		i--
		digits[i] = byte('0' + v)
	}
	b.buf = append(b.buf, digits[i:]...)
}

// pair returns the two digits of v, which is below 100.
func pair[I int | uint64](v I) (tens, units byte) {
	return digitPairs[v*pairLen], digitPairs[v*pairLen+1]
}

// Bounds of the plain notation encoding/json writes floats in; exponent
// notation covers the magnitudes below the first and from the second.
const (
	minPlainFloat = 1e-6
	maxPlainFloat = 1e21
)

// shortestIntLimit is 2^54+8, the least integral float64 whose shortest form is not
// its integer: its rounding interval holds 18014398509481990. The shortest form of
// every lower integral float64 is its integer, digit for digit.
const shortestIntLimit = 18014398509481992

// The precisions strconv formats and parses floats at.
const (
	float64Bits = 64
	float32Bits = 32
)

// appendFloat64 writes v as encoding/json does: the shortest form that reads
// back as v, exponent notation outside the plain bounds, and null for NaN and
// ±Inf. Non-negative integral values below shortestIntLimit take the integer writer.
func (b *Builder) appendFloat64(v float64) {
	abs := math.Abs(v)
	switch {
	case math.IsNaN(v) || math.IsInf(v, 0):
		b.buf = append(b.buf, litNull...)
	case abs < shortestIntLimit && v == math.Trunc(v) && !math.Signbit(v):
		b.appendUint64(uint64(v))
	case abs == 0 || (abs >= minPlainFloat && abs < maxPlainFloat):
		b.buf = strconv.AppendFloat(b.buf, v, 'f', -1, float64Bits)
	default:
		b.buf = strconv.AppendFloat(b.buf, v, 'e', -1, float64Bits)
		b.trimExponentZero()
	}
}

// trimExponentZero rewrites a trailing e-0X of the exponent form just written
// as e-X, as encoding/json does; strconv pads the exponent to two digits.
func (b *Builder) trimExponentZero() {
	exp := len(b.buf) - len("e-00")
	if string(b.buf[exp:exp+len("e-0")]) == "e-0" {
		b.buf = append(b.buf[:exp+len("e-")], b.buf[exp+len("e-0")])
	}
}
