package jsonfast

import (
	"bytes"
	"math"
	"math/bits"
	"strconv"
)

// DecodeBool decodes the JSON literal true or false.
func DecodeBool[T Text](raw T) (value, ok bool) {
	switch stringOf(raw) {
	case litTrue:
		return true, true
	case litFalse:
		return false, true
	}
	return false, false
}

// DecodeInt64 decodes a JSON integer: no fraction, no exponent, no leading '+'
// and no leading zero other than in 0 and -0; an integer beyond int64, like a
// text that is not one integer, gives 0 and false.
func DecodeInt64[T Text](raw T) (int64, bool) {
	d := bytesOf(raw)
	if !isInteger(d) {
		return 0, false
	}
	negative, digits := d[0] == '-', d
	if negative {
		digits = d[1:]
	}
	switch v, ok := digitsValue(digits); {
	case !ok:
		return 0, false
	case v <= math.MaxInt64 && negative:
		return -int64(v), true
	case v <= math.MaxInt64:
		return int64(v), true
	case negative && v == -math.MinInt64:
		return math.MinInt64, true
	}
	return 0, false
}

// DecodeUint64 decodes a JSON integer: no sign, no fraction, no exponent and no
// leading zero other than in 0; an integer beyond uint64, like a text that is not
// one such integer, gives 0 and false.
func DecodeUint64[T Text](raw T) (uint64, bool) {
	d := bytesOf(raw)
	if !isInteger(d) || d[0] == '-' {
		return 0, false
	}
	return digitsValue(d)
}

// isInteger reports whether d is exactly an integer of the number grammar.
func isInteger(d []byte) bool {
	end, ok := skipInteger(d, 0)
	return ok && end == len(d)
}

// digitsValue returns the value of the decimal digits d, and false when it
// passes uint64.
func digitsValue(d []byte) (uint64, bool) {
	var v uint64
	for _, c := range d {
		hi, lo := bits.Mul64(v, decimalBase)
		sum, carry := bits.Add64(lo, uint64(c-'0'), 0)
		if hi|carry != 0 {
			return 0, false
		}
		v = sum
	}
	return v, true
}

// DecodeFloat64 decodes a JSON number into the nearest float64 and never
// allocates; a value beyond float64, like a text that is not one number, gives
// 0 and false.
func DecodeFloat64[T Text](raw T) (float64, bool) {
	return decodeFloat(bytesOf(raw), float64Bits, float64Limit)
}

// DecodeFloat32 decodes a JSON number into the nearest float32, in one rounding,
// and never allocates; a value beyond float32, like a text that is not one number,
// gives 0 and false.
func DecodeFloat32[T Text](raw T) (float32, bool) {
	v, ok := decodeFloat(bytesOf(raw), float32Bits, float32Limit)
	return float32(v), ok
}

// The least magnitudes that strconv rounds past float64 and float32,
// (2^54-1)·2^970 and (2^25-1)·2^103, in decimal digits; both end in a
// nonzero digit.
const (
	float64Limit = "179769313486231580793728971405303415079934132710037826936173778980444968" +
		"292764750946649017977587207096330286416692887910946555547851940402630657488671505820" +
		"681908902000708383676273854845817711531764475730270069855571366959622842914819860834" +
		"936475292719074168444365510704342711559699508093042880177904174497792"
	float32Limit = "340282356779733661637539395458142568448"
)

// decodeFloat decodes the number d at the precision of bitSize. It rejects the
// magnitudes from limit up before strconv, which reports their overflow with an
// allocation, and hands strconv a text it reads exactly.
func decodeFloat(d []byte, bitSize int, limit string) (float64, bool) {
	s, ok := numberAt(d, 0)
	if !ok || s.end != len(d) {
		return 0, false
	}
	switch n := decimalOf(d, s); {
	case !n.below(limit):
		return 0, false
	case !n.exact():
		return n.parse(bitSize)
	}
	v, err := strconv.ParseFloat(textOf[string](d), bitSize)
	return v, err == nil
}

// decimalForm is a number as its sign, its significant digits, first and then
// second with no leading zero, the place of its point among them, and an
// exponent that saturates where no float tells it from a larger one.
type decimalForm struct {
	sign, first, second []byte
	point               int
	exponent            int64
}

// decimalOf reads the number d whose parts s locates.
func decimalOf(d []byte, s numberSpan) decimalForm {
	n := decimalForm{first: d[:s.intEnd], second: d[min(s.intEnd+1, s.fracEnd):s.fracEnd]}
	if n.first[0] == '-' {
		n.sign, n.first = n.first[:1], n.first[1:]
	}
	n.point = len(n.first)
	if n.first[0] == '0' {
		lead := bytes.TrimLeft(n.second, "0")
		n.first, n.second, n.point = lead, nil, len(lead)-len(n.second)
	}
	n.exponent = exponentOf(d[s.fracEnd:s.end], int64(len(d))+misreadExponent)
	return n
}

// below reports whether the magnitude of n is less than the integer whose
// decimal digits are limit.
func (n *decimalForm) below(limit string) bool {
	x, digits := int64(n.point)+n.exponent, int64(len(limit))
	switch {
	case len(n.first) == 0 || x < digits:
		return true
	case x > digits:
		return false
	}
	return lessDigits(n.first, n.second, limit)
}

// strconv keeps pointDigits significant digits, and reads the point of a number
// exactly only below misreadPoint digits before it, and its exponent only below
// misreadExponent, where it stops adding exponent digits.
const (
	pointDigits     = 800
	misreadPoint    = 801
	misreadExponent = 100000
)

// exact reports whether strconv reads the text of n exactly. A negative exponent
// from misreadExponent on needs no care: with at most pointDigits digits before
// its point, n then rounds to zero however strconv reads the exponent.
func (n *decimalForm) exact() bool {
	return n.point < misreadPoint && n.exponent < misreadExponent
}

// canonicalLen is the longest canonical text: a sign, "0.", pointDigits digits,
// a sticky digit and an exponent from zeroExponent up.
const canonicalLen = len("-0.") + pointDigits + len("1e-400")

// zeroExponent is an exponent at which 0.digits rounds to zero at every float
// size, as it does at any lower one.
const zeroExponent = -400

// parse parses n as strconv reads [-]0.digits e x exactly: the first pointDigits
// significant digits, then a 1 when a later one is not 0, which no rounding
// tells from the rest of the digits.
func (n *decimalForm) parse(bitSize int) (float64, bool) {
	var buf [canonicalLen]byte
	head := min(len(n.first), pointDigits)
	tail := min(len(n.second), pointDigits-head)
	text := append(buf[:0], n.sign...)
	text = append(text, "0."...)
	text = append(text, n.first[:head]...)
	text = append(text, n.second[:tail]...)
	if nonZero(n.first[head:]) || nonZero(n.second[tail:]) {
		text = append(text, '1')
	}
	text = append(text, 'e')
	text = strconv.AppendInt(text, max(int64(n.point)+n.exponent, zeroExponent), decimalBase)
	v, err := strconv.ParseFloat(textOf[string](text), bitSize)
	return v, err == nil
}

// nonZero reports whether a digit of d is not 0.
func nonZero(d []byte) bool {
	return len(bytes.TrimLeft(d, "0")) > 0
}

// exponentOf returns the exponent that the exponent part d of a number, which
// may be empty, gives, saturated at bound; int64 keeps 32-bit targets exact.
func exponentOf(d []byte, bound int64) int64 {
	if len(d) == 0 {
		return 0
	}
	sign, digits := int64(1), d[1:]
	switch digits[0] {
	case '-':
		sign, digits = -1, digits[1:]
	case '+':
		digits = digits[1:]
	}
	var e int64
	for _, c := range digits {
		e = min(e*decimalBase+int64(c-'0'), bound)
	}
	return sign * e
}

// lessDigits reports whether the digits of first and then second, read as
// the leading digits of a number of len(limit) digits, spell less than limit.
func lessDigits(first, second []byte, limit string) bool {
	for k := range len(limit) {
		var c byte
		switch {
		case k < len(first):
			c = first[k]
		case k-len(first) < len(second):
			c = second[k-len(first)]
		default:
			return true
		}
		switch {
		case c < limit[k]:
			return true
		case c > limit[k]:
			return false
		}
	}
	return false
}

// IsNumber reports whether raw is exactly one JSON number, so a DecodeFloat64
// failure on it means the value lies beyond float64.
func IsNumber[T Text](raw T) bool {
	d := bytesOf(raw)
	end, ok := skipNumber(d, 0)
	return ok && end == len(d)
}
