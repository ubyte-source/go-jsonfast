package jsonfast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// unmarshalScalar decodes raw into v with encoding/json when raw is exactly
// one token that is not null, which encoding/json would skip.
func unmarshalScalar(raw []byte, v any) bool {
	return len(raw) > 0 && bytes.Equal(bytes.TrimSpace(raw), raw) && !bytes.Equal(raw, []byte(litNull)) &&
		json.Unmarshal(raw, v) == nil
}

func TestDecodeBoolAcceptsOnlyTheTwoLiterals(t *testing.T) {
	for _, tc := range []struct {
		in       string
		want, ok bool
	}{
		{litTrue, true, true},
		{litFalse, false, true},
		{"True", false, false},
		{"FALSE", false, false},
		{"tru", false, false},
		{"falsex", false, false},
		{" true", false, false},
		{"1", false, false},
		{litNull, false, false},
		{"", false, false},
	} {
		got, ok := DecodeBool(tc.in)
		gotBytes, okBytes := DecodeBool([]byte(tc.in))
		if ok != tc.ok || got != tc.want || okBytes != ok || gotBytes != got {
			t.Errorf("DecodeBool(%q) = %v, %v and %v, %v; want %v, %v",
				tc.in, got, ok, gotBytes, okBytes, tc.want, tc.ok)
		}
	}
}

func FuzzDecodeBool(f *testing.F) {
	for _, s := range []string{litTrue, litFalse, "t", " true", litNull} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		got, ok := DecodeBool(raw)
		var std bool
		if stdOK := unmarshalScalar(raw, &std); !sameScalar(got, ok, std, stdOK) {
			t.Fatalf("DecodeBool(%q) = %v, %v; encoding/json = %v, %v", raw, got, ok, std, stdOK)
		}
	})
}

// negative returns the number text s with a minus sign.
func negative(s string) string {
	return minus + s
}

// minus is the sign of a negative number.
const minus = "-"

// tenTo returns 10 to the power n in decimal digits.
func tenTo(n int) string {
	return "1" + digits('0', n)
}

// digits returns n copies of the digit d.
func digits(d byte, n int) string {
	return strings.Repeat(string([]byte{d}), n)
}

// integerCases returns integer texts that DecodeInt64 or DecodeUint64 may
// accept, around both bounds and past them.
func integerCases() []string {
	return []string{
		"0", "-0", "7", "42", "-42", "9223372036854775807", "-9223372036854775807", "-9223372036854775808",
		"9223372036854775808", "-9223372036854775809", "18446744073709551615", "18446744073709551616",
		"-18446744073709551615", "99999999999999999999", "-99999999999999999999", digits('9', manyDigits),
		tenTo(manyDigits), negative(digits('9', manyDigits)), "", "-", "+1", "01", "-01", "00", "1.5",
		"1e5", "1 ", " 1", "0x1", "１",
	}
}

func TestDecodeInt64MatchesStrconv(t *testing.T) {
	for _, in := range integerCases() {
		want, err := strconv.ParseInt(in, decimalRadix, intBits)
		wantOK := err == nil && integerGrammar().MatchString(in)
		got, ok := DecodeInt64([]byte(in))
		gotString, okString := DecodeInt64(in)
		if !sameScalar(got, ok, want, wantOK) || okString != ok || gotString != got {
			t.Errorf("DecodeInt64(%q) = %d, %v and %d, %v; strconv = %d, %v",
				in, got, ok, gotString, okString, want, err)
		}
	}
}

func FuzzDecodeInt64(f *testing.F) {
	for _, s := range integerCases() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, ok := DecodeInt64(s)
		var std int64
		if stdOK := unmarshalScalar([]byte(s), &std); !sameScalar(got, ok, std, stdOK) {
			t.Fatalf("DecodeInt64(%q) = %d, %v; encoding/json = %d, %v", s, got, ok, std, stdOK)
		}
	})
}

func TestDecodeUint64MatchesStrconv(t *testing.T) {
	for _, in := range integerCases() {
		want, err := strconv.ParseUint(in, decimalRadix, intBits)
		wantOK := err == nil && integerGrammar().MatchString(in)
		got, ok := DecodeUint64(in)
		gotBytes, okBytes := DecodeUint64([]byte(in))
		if !sameScalar(got, ok, want, wantOK) || okBytes != ok || gotBytes != got {
			t.Errorf("DecodeUint64(%q) = %d, %v and %d, %v; strconv = %d, %v",
				in, got, ok, gotBytes, okBytes, want, err)
		}
	}
	if got, ok := DecodeUint64("-0"); !sameScalar(got, ok, 0, false) {
		t.Errorf("DecodeUint64(\"-0\") = %d, %v, want 0, false for the minus sign", got, ok)
	}
}

func FuzzDecodeUint64(f *testing.F) {
	for _, s := range integerCases() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, ok := DecodeUint64(s)
		var std uint64
		if stdOK := unmarshalScalar([]byte(s), &std); !sameScalar(got, ok, std, stdOK) {
			t.Fatalf("DecodeUint64(%q) = %d, %v; encoding/json = %d, %v", s, got, ok, std, stdOK)
		}
	})
}

// limitPrecision is the precision roundingLimit adds at, room for the largest
// float64 and half of its unit.
const limitPrecision = 64

// roundingLimit returns, in decimal digits, the least magnitude that rounds
// past largest, the largest value of a float whose next value down is lower.
func roundingLimit(largest, lower float64) string {
	limit := new(big.Float).SetPrec(limitPrecision).SetFloat64(largest)
	limit.Add(limit, new(big.Float).SetFloat64((largest-lower)/2))
	return limit.Text('f', 0)
}

// floatCases returns number texts around the float bounds of limit, the digits
// roundingLimit gives, and away from them.
func floatCases(limit string) []string {
	under := limit[:len(limit)-1] + string(limit[len(limit)-1]-1)
	// bumped passes limit by one in its second digit, a lower tail after it.
	bumped := limit[:1] + string(limit[1]+1) + digits('0', len(limit)-2)
	top := len(limit) - 1
	scaled := func(mantissa, tail string, exp int) string {
		return fmt.Sprintf("%s.%s%se%d", mantissa[:1], mantissa[1:], tail, exp)
	}
	return []string{
		limit, under, negative(limit), negative(under), limit + ".0", under + ".999999999999999999999",
		bumped, fmt.Sprintf("9e%d", top-1),
		digits('9', len(limit)), negative(digits('9', len(limit))) + ".5",
		scaled(limit, "", top), scaled(under, "9999", top), scaled(limit, digits('0', manyDigits), top),
		under + digits('9', manyDigits), fmt.Sprintf("0.%se%d", limit, top+1),
		fmt.Sprintf("0.0%se%d", limit[:strings.IndexByte(limit, '0')], top+len("0.")),
		fmt.Sprintf("0.00%se+0%d", under, top+len("0.0")), fmt.Sprintf("1e%d", top), fmt.Sprintf("1e%d", top+1),
		"1e" + digits('9', manyDigits), "1e-" + digits('9', manyDigits), negative("1e" + digits('9', manyDigits)),
		negative("1e-" + digits('9', manyDigits)),
		"1e00000000000000000000000000000000000000400", "1e-00000000000000000000000000000400", "-1e400",
		digits('9', manyDigits), "0." + digits('0', manyDigits) + "1", "0", "-0", "3.14", "-2.5", "1e10",
		"1.5E-3", "1E+2", "1e-400", "0.0e999", "", "NaN", "Inf", "+1", ".5", "1.", "1e", "1e+", "0x10", "1_000", " 1",
	}
}

func TestDecodeFloat64MatchesStrconv(t *testing.T) {
	limit := roundingLimit(math.MaxFloat64, math.Nextafter(math.MaxFloat64, 0))
	for _, in := range floatCases(limit) {
		want, err := strconv.ParseFloat(in, float64Bits)
		wantOK := err == nil && numberGrammar().MatchString(in)
		got, ok := DecodeFloat64(in)
		gotBytes, okBytes := DecodeFloat64([]byte(in))
		if ok != wantOK || ok && !sameBits(got, want) || !ok && got != 0 || okBytes != ok || !sameBits(gotBytes, got) {
			t.Errorf("DecodeFloat64(%.40q) = %g, %v and %g, %v; strconv = %g, %v",
				in, got, ok, gotBytes, okBytes, want, err)
		}
	}
}

// nearestFloat returns the float of bitSize nearest to the JSON number s, and
// false when s lies beyond it, as big.Rat reads s exactly; an exponent past the
// 10^6 big.Rat takes leaves s beyond every float, or below it as its sign says.
func nearestFloat(s string, bitSize int) (float64, bool) {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		mantissa, exponent, _ := strings.Cut(strings.ToLower(s), "e")
		if strings.Trim(mantissa, "-0.") != "" && !strings.HasPrefix(exponent, minus) {
			return 0, false
		}
		if strings.HasPrefix(mantissa, minus) {
			return math.Copysign(0, -1), true
		}
		return 0, true
	}
	f, _ := r.Float64()
	if bitSize == float32Bits {
		f32, _ := r.Float32()
		f = float64(f32)
	}
	if math.IsInf(f, 0) {
		return 0, false
	}
	if f == 0 && strings.HasPrefix(s, minus) {
		return math.Copysign(0, -1), true
	}
	return f, true
}

// sameBits reports whether a and b are the same float64, sign of zero included.
func sameBits(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b)
}

// fuzzFloat checks what decode makes of s at bitSize against nearestFloat.
func fuzzFloat(t *testing.T, grammar *regexp.Regexp, s string, bitSize int, decode func(string) (float64, bool)) {
	t.Helper()
	got, ok := decode(s)
	want, wantOK := 0.0, false
	if grammar.MatchString(s) {
		want, wantOK = nearestFloat(s, bitSize)
	}
	if ok != wantOK || !sameBits(got, want) {
		t.Fatalf("decoding %.60q at %d bits = %g, %v; big.Rat = %g, %v", s, bitSize, got, ok, want, wantOK)
	}
}

func FuzzDecodeFloat64(f *testing.F) {
	for _, s := range floatCases(roundingLimit(math.MaxFloat64, math.Nextafter(math.MaxFloat64, 0))) {
		f.Add(s)
	}
	for _, tc := range misreadNumbers(manyDigits) {
		f.Add(tc.long)
	}
	grammar := numberGrammar()
	f.Fuzz(func(t *testing.T, s string) {
		fuzzFloat(t, grammar, s, float64Bits, DecodeFloat64[string])
	})
}

func TestDecodeFloat32RoundsOnceAndRejectsOverflow(t *testing.T) {
	limit := roundingLimit(math.MaxFloat32, float64(math.Nextafter32(math.MaxFloat32, 0)))
	for _, in := range floatCases(limit) {
		want, err := strconv.ParseFloat(in, float32Bits)
		wantOK := err == nil && numberGrammar().MatchString(in)
		got, ok := DecodeFloat32([]byte(in))
		gotString, okString := DecodeFloat32(in)
		if ok != wantOK || ok && !sameBits(float64(got), float64(float32(want))) || !ok && got != 0 || okString != ok ||
			!sameBits(float64(gotString), float64(got)) {
			t.Errorf("DecodeFloat32(%.40q) = %g, %v and %g, %v; strconv = %g, %v",
				in, got, ok, gotString, okString, want, err)
		}
	}
}

// Digit counts of the long numbers: manyDigits passes every bound; strconv may
// misread the point past pointRead digits and an exponent of sixDigits or more,
// and saturates its exponent from expRead on.
const (
	manyDigits = 1 << 10
	pointRead  = 800
	pastPoint  = 900
	expRead    = 10000
	sixDigits  = 100000
)

// misreadNumber is a number that strconv may read inexactly, with a short text
// that strconv reads exactly and that rounds to the same float at every size.
type misreadNumber struct {
	long, short string
}

// misreadNumbers returns the numbers with an exponent from expRead up or more
// than pointRead digits before the point, and those just inside both bounds,
// that are at most limit bytes long.
func misreadNumbers(limit int) []misreadNumber {
	tiny := func(zeros int, exp string) string { return fmt.Sprintf("0.%s1%s", digits('0', zeros), exp) }
	const half = "9007199254740993" // 2^53+1, halfway between two float64 values
	// tie is (2^53-5)*2^-1075, halfway between two subnormal float64 values, times
	// 10^1075: an integer of 768 digits, all of which rounding to even reads.
	const tieOdd, tieShift = 1<<53 - 5, 1075
	tie := new(big.Int).Exp(big.NewInt(decimalBase), big.NewInt(tieShift), nil)
	tie.Rsh(tie.Mul(tie, new(big.Int).SetUint64(tieOdd)), tieShift)
	all := []misreadNumber{
		{tenTo(pointRead-1) + "e-799", "0.1e1"},
		{tenTo(pointRead) + "e-800", "1.0"},
		{tiny(sixDigits-2, "e99999"), "1e0"},
		{tiny(sixDigits-1, "e100000"), "10e-1"},
		{tenTo(pointRead-1) + "e-490", "1e309"},
		{tenTo(pointRead) + "e-490", "1e310"},
		{tenTo(pastPoint) + "e-590", "1e310"},
		{tenTo(pastPoint) + "e-800", "1e100"},
		{tenTo(manyDigits) + "e-600", "1e424"},
		{tenTo(manyDigits) + "e-1024", "1.0"},
		{digits('9', manyDigits) + "e-1024", "0.99999999999999999999999"},
		{"2" + digits('0', manyDigits) + "." + digits('0', manyDigits) + "e-492", "2e532"},
		{half + digits('0', pastPoint) + "e-900", half},
		{half + digits('0', pastPoint) + "1e-901", half + ".0001"},
		{negative(half + digits('0', pastPoint) + "1e-901"), negative(half + ".0001")},
		{half + digits('0', pastPoint) + ".1e-900", half + ".0001"},
		{tie.String() + digits('0', pastPoint) + "e-1975", tie.String() + "e-1075"},
		{tenTo(pastPoint) + "." + digits('0', manyDigits) + "e-890", "1e+10"},
		{negative(digits('9', pastPoint)) + "e-99999", "-0.0"},
		{"0e99999", "0.0"},
		{tenTo(expRead/2) + "e-4990", "1E10"},
		{tiny(expRead, "e10010"), "1e9"},
		{negative(tiny(expRead, "e10010")), "-1e9"},
		{tiny(expRead, "e-10000"), "0e0"},
		{negative(tiny(expRead, "e-10000")), "-0e5"},
		{"1e123456", "1e500"},
		{"1e-123456", "0.00"},
		{"0.00001e100004", "1e99999"},
		{tiny(sixDigits, "e150000"), "1e49999"},
		{tiny(sixDigits, "e100050"), "1e49"},
		{tiny(sixDigits-1, "e100010"), "10000000000"},
		{tenTo(2*sixDigits) + "e-199900", "1e100"},
		{digits('9', hugeDigits), "9e999"},
		{negative(digits('9', hugeDigits)), "-1e400"},
	}
	return slices.DeleteFunc(all, func(tc misreadNumber) bool { return len(tc.long) > limit })
}

// sameFloat reports whether a decoder's got and ok match what strconv makes of
// a text it reads exactly: its value and sign, or 0 and false on a range error.
func sameFloat(got float64, ok bool, want float64, err error) bool {
	if err != nil {
		return !ok && got == 0
	}
	return ok && sameBits(got, want)
}

func TestDecodeFloat64ReadsWhatStrconvMisreads(t *testing.T) {
	for _, tc := range misreadNumbers(hugeDigits + 1) {
		want, err := strconv.ParseFloat(tc.short, float64Bits)
		got, ok := DecodeFloat64(tc.long)
		want32, err32 := strconv.ParseFloat(tc.short, float32Bits)
		got32, ok32 := DecodeFloat32(tc.long)
		if !sameFloat(got, ok, want, err) || !sameFloat(float64(got32), ok32, want32, err32) {
			t.Errorf("DecodeFloat64 and DecodeFloat32 of %.40q = %g, %v and %g, %v; want those of %s",
				tc.long, got, ok, got32, ok32, tc.short)
		}
	}
}

func TestDecodeFloat32RoundsHalfwayToEven(t *testing.T) {
	if got, ok := DecodeFloat32("1.00000005960464477539062500001"); !ok || got != math.Nextafter32(1, 2) {
		t.Errorf("DecodeFloat32 rounded a value just past the halfway point to %g, %v, want %g, true", got, ok,
			math.Nextafter32(1, 2))
	}
	if got, ok := DecodeFloat32("1.000000059604644775390625"); !ok || got != 1 {
		t.Errorf("DecodeFloat32 rounded the halfway point to %g, %v, want the even 1", got, ok)
	}
}

// The bound and exponents of TestExponentOfSaturatesAtItsBound, past what a
// 32-bit int holds once multiplied by ten.
const (
	wideBound    = 250_010_000
	wideExponent = 2_200_000_000
)

func TestExponentOfSaturatesAtItsBound(t *testing.T) {
	for _, tc := range []struct {
		part string
		want int64
	}{
		{"", 0}, {"e1", 1}, {"E+2", 2}, {"e-002", -2},
		{"e" + strconv.FormatInt(wideExponent, decimalRadix), wideBound},
		{"e-" + strconv.FormatInt(wideExponent, decimalRadix), -wideBound},
		{"e" + strconv.FormatInt(wideBound, decimalRadix), wideBound},
	} {
		if got := exponentOf([]byte(tc.part), wideBound); got != tc.want {
			t.Errorf("exponentOf(%q, %d) = %d, want %d", tc.part, wideBound, got, tc.want)
		}
	}
}

func FuzzDecodeFloat32(f *testing.F) {
	for _, s := range floatCases(roundingLimit(math.MaxFloat32, float64(math.Nextafter32(math.MaxFloat32, 0)))) {
		f.Add(s)
	}
	for _, tc := range misreadNumbers(manyDigits) {
		f.Add(tc.long)
	}
	grammar := numberGrammar()
	decode := func(s string) (float64, bool) {
		v, ok := DecodeFloat32(s)
		return float64(v), ok
	}
	f.Fuzz(func(t *testing.T, s string) {
		fuzzFloat(t, grammar, s, float32Bits, decode)
	})
}

func TestDecodeBoolAndNumbersAllocs(t *testing.T) {
	huge := []byte(digits('9', hugeDigits))
	assertAllocs(t, 0, func() {
		_, _ = DecodeBool([]byte(litTrue))
		_, _ = DecodeInt64("-9223372036854775808")
		_, _ = DecodeUint64([]byte("18446744073709551615"))
		_, _ = DecodeFloat64("-12.5e3")
		_, _ = DecodeFloat32([]byte("3.25"))
		_ = IsNumber("-0.5e-7")
	})
	misread := misreadNumbers(hugeDigits + 1)
	cases64 := floatCases(roundingLimit(math.MaxFloat64, math.Nextafter(math.MaxFloat64, 0)))
	cases32 := floatCases(roundingLimit(math.MaxFloat32, float64(math.Nextafter32(math.MaxFloat32, 0))))
	assertAllocs(t, 0, func() {
		for _, tc := range misread {
			_, _ = DecodeFloat64(tc.long)
			_, _ = DecodeFloat32(tc.long)
		}
		for _, in := range cases64 {
			_, _ = DecodeFloat64(in)
		}
		for _, in := range cases32 {
			_, _ = DecodeFloat32(in)
		}
	})
	assertAllocs(t, 0, func() {
		_, _ = DecodeInt64(huge)
		_, _ = DecodeInt64("-99999999999999999999")
		_, _ = DecodeUint64(huge)
		_, _ = DecodeUint64("-99999999999999999999")
	})
}

func BenchmarkDecodeBool(b *testing.B) {
	raw := []byte(litFalse)
	if got, ok := DecodeBool(raw); got || !ok {
		b.Fatalf("DecodeBool(%s) = %v, %v, want false, true", raw, got, ok)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = DecodeBool(raw)
	}
}

// The text the DecodeInt64 benchmarks read and its value, -(2^53+1), one past the
// integers a float64 holds exactly.
const (
	benchInt64            = "-9007199254740993"
	benchInt64Value int64 = -(1<<53 + 1)
)

// benchUint64 is the text the DecodeUint64 benchmarks read, math.MaxUint64.
const benchUint64 = "18446744073709551615"

func BenchmarkDecodeInt64(b *testing.B) {
	raw := []byte(benchInt64)
	if got, ok := DecodeInt64(raw); got != benchInt64Value || !ok {
		b.Fatalf("DecodeInt64(%s) = %d, %v, want %d, true", raw, got, ok, benchInt64Value)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = DecodeInt64(raw)
	}
}

// BenchmarkDecodeInt64Strconv reads the text of BenchmarkDecodeInt64 with
// strconv.ParseInt, which DecodeInt64 stands in for on JSON integers.
func BenchmarkDecodeInt64Strconv(b *testing.B) {
	got, err := strconv.ParseInt(benchInt64, decimalRadix, intBits)
	if got != benchInt64Value || err != nil {
		b.Fatalf("strconv.ParseInt(%s) = %d, %v, want %d, nil", benchInt64, got, err, benchInt64Value)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, err = strconv.ParseInt(benchInt64, decimalRadix, intBits)
	}
	noError(b, err)
}

func BenchmarkDecodeUint64(b *testing.B) {
	raw := []byte(benchUint64)
	if got, ok := DecodeUint64(raw); got != math.MaxUint64 || !ok {
		b.Fatalf("DecodeUint64(%s) = %d, %v, want %d, true", raw, got, ok, uint64(math.MaxUint64))
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = DecodeUint64(raw)
	}
}

// BenchmarkDecodeUint64Strconv reads the text of BenchmarkDecodeUint64 with
// strconv.ParseUint, which DecodeUint64 stands in for on JSON integers.
func BenchmarkDecodeUint64Strconv(b *testing.B) {
	got, err := strconv.ParseUint(benchUint64, decimalRadix, intBits)
	if got != math.MaxUint64 || err != nil {
		b.Fatalf("strconv.ParseUint(%s) = %d, %v, want %d, nil", benchUint64, got, err, uint64(math.MaxUint64))
	}
	b.ReportAllocs()
	for b.Loop() {
		_, err = strconv.ParseUint(benchUint64, decimalRadix, intBits)
	}
	noError(b, err)
}

func BenchmarkDecodeFloat64(b *testing.B) {
	raw := []byte("-0.0123456789e-3")
	want, err := strconv.ParseFloat(string(raw), float64Bits)
	noError(b, err)
	if got, ok := DecodeFloat64(raw); got != want || !ok {
		b.Fatalf("DecodeFloat64(%s) = %v, %v, want %v, true", raw, got, ok, want)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = DecodeFloat64(raw)
	}
}

func BenchmarkDecodeFloat32(b *testing.B) {
	raw := []byte("-0.0123456789")
	want, err := strconv.ParseFloat(string(raw), float32Bits)
	noError(b, err)
	if got, ok := DecodeFloat32(raw); got != float32(want) || !ok {
		b.Fatalf("DecodeFloat32(%s) = %v, %v, want %v, true", raw, got, ok, float32(want))
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = DecodeFloat32(raw)
	}
}

// numberGrammar is the number rule written as a regular expression.
func numberGrammar() *regexp.Regexp {
	return regexp.MustCompile(`^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?$`)
}

// integerGrammar is the number rule without fraction and exponent.
func integerGrammar() *regexp.Regexp {
	return regexp.MustCompile(`^-?(0|[1-9]\d*)$`)
}

func TestIsNumberFollowsTheNumberGrammar(t *testing.T) {
	grammar := numberGrammar()
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"0", true}, {"-0.0", true}, {"10", true}, {"0.5", true}, {"1e5", true},
		{"1E-5", true}, {"1e+05", true}, {"-1.25e10", true}, {"9", true},
		{"", false}, {"--0", false}, {"01", false}, {"+1", false}, {".5", false},
		{"1.", false}, {"1e", false}, {"1e+", false}, {"1.e5", false}, {"-a", false},
		{" 1", false}, {"1 ", false}, {`"1"`, false}, {"NaN", false}, {"1x", false},
	} {
		if got := IsNumber(tc.in); got != tc.want {
			t.Errorf("IsNumber(%q) = %v, want %v", tc.in, got, tc.want)
		}
		if grammar.MatchString(tc.in) != tc.want {
			t.Errorf("the regular expression matches %q: %v, want %v as the table says", tc.in, !tc.want, tc.want)
		}
	}
}

func FuzzIsNumber(f *testing.F) {
	for _, s := range []string{"-1.5e+3", "01", "3.", "--1", "4e"} {
		f.Add(s)
	}
	grammar := numberGrammar()
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := IsNumber(s), grammar.MatchString(s); got != want {
			t.Fatalf("IsNumber(%q) = %v, want %v", s, got, want)
		}
	})
}

func BenchmarkIsNumber(b *testing.B) {
	raw := []byte("-1234567.890e-12")
	if !IsNumber(raw) || !json.Valid(raw) {
		b.Fatalf("IsNumber(%s) = false, want true as encoding/json reads it", raw)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = IsNumber(raw)
	}
}

func ExampleIsNumber() {
	fmt.Println(IsNumber("1e400"), IsNumber("1."))
	_, ok := DecodeFloat64("1e400")
	fmt.Println(ok)
	// Output:
	// true false
	// false
}
