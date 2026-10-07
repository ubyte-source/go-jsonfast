package jsonfast

const (
	litTrue  = "true"
	litFalse = "false"
	litNull  = "null"
)

// wsBits has the bit of each JSON whitespace byte: space, tab, LF and CR.
const wsBits uint64 = 1<<' ' | 1<<'\t' | 1<<'\n' | 1<<'\r'

// isSpace reports whether c is JSON whitespace.
func isSpace(c byte) bool {
	return c <= ' ' && wsBits>>c&1 == 1
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

// skipWS returns the index of the first byte at or after data[i] that is not
// JSON whitespace, or len(data).
func skipWS(data []byte, i int) int {
	for _, c := range data[i:] {
		if !isSpace(c) {
			return i
		}
		i++
	}
	return i
}

// TrimWS returns data without the JSON whitespace at either end.
func TrimWS[T Text](data T) T {
	d := bytesOf(data)
	i, j := skipWS(d, 0), len(d)
	if i == j {
		return view[T](d, j, j)
	}
	// d[i] is no whitespace, so the scan back stops at it.
	for isSpace(d[j-1]) {
		j--
	}
	return view[T](d, i, j)
}

// skipValueAt scans past the JSON value that starts at data[i], after optional
// whitespace, and returns the index past it. Strings, numbers and literals are
// checked in full; an array or object counts only its own opener and closer.
func skipValueAt(data []byte, i int) (int, bool) {
	i = skipWS(data, i)
	if i >= len(data) {
		return 0, false
	}
	switch data[i] {
	case '"':
		return skipStringAt(data, i)
	case '{', '[':
		return skipBraced(data, i)
	}
	return skipScalar(data, i)
}

// skipScalar skips the number or the literal that starts at data[i].
func skipScalar(data []byte, i int) (int, bool) {
	switch data[i] {
	case 't':
		return skipLiteral(data, i, litTrue)
	case 'f':
		return skipLiteral(data, i, litFalse)
	case 'n':
		return skipLiteral(data, i, litNull)
	}
	return skipNumber(data, i)
}

func skipLiteral(data []byte, i int, lit string) (int, bool) {
	if len(data)-i < len(lit) || string(data[i:i+len(lit)]) != lit {
		return 0, false
	}
	return i + len(lit), true
}

func skipNumber(data []byte, i int) (int, bool) {
	s, ok := numberAt(data, i)
	return s.end, ok
}

// numberSpan locates the parts of a number: its sign and integer part end at
// intEnd, its fraction at fracEnd and its exponent at end.
type numberSpan struct {
	intEnd, fracEnd, end int
}

// numberAt reads -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)? at data[i].
func numberAt(data []byte, i int) (numberSpan, bool) {
	var (
		s  numberSpan
		ok bool
	)
	if s.intEnd, ok = skipInteger(data, i); !ok {
		return numberSpan{}, false
	}
	if s.fracEnd, ok = skipFraction(data, s.intEnd); !ok {
		return numberSpan{}, false
	}
	if s.end, ok = skipExponent(data, s.fracEnd); !ok {
		return numberSpan{}, false
	}
	return s, true
}

// skipInteger skips the sign and the integer part of a number.
func skipInteger(data []byte, i int) (int, bool) {
	j := i
	if j != len(data) && data[j] == '-' {
		j++
	}
	switch {
	case j == len(data):
		return 0, false
	case data[j] == '0':
		return j + 1, true
	case isDigit(data[j]):
		return skipDigits(data, j+1), true
	}
	return 0, false
}

// skipFraction skips the fraction of a number, when there is one.
func skipFraction(data []byte, i int) (int, bool) {
	if i == len(data) || data[i] != '.' {
		return i, true
	}
	return skipDigitRun(data, i+1)
}

// skipExponent skips the exponent of a number, when there is one.
func skipExponent(data []byte, i int) (int, bool) {
	if i == len(data) || (data[i] != 'e' && data[i] != 'E') {
		return i, true
	}
	j := i + 1
	if j != len(data) && (data[j] == '+' || data[j] == '-') {
		j++
	}
	return skipDigitRun(data, j)
}

// skipDigitRun skips one digit or more.
func skipDigitRun(data []byte, i int) (int, bool) {
	if j := skipDigits(data, i); j > i {
		return j, true
	}
	return 0, false
}

func skipDigits(data []byte, i int) int {
	for i < len(data) && isDigit(data[i]) {
		i++
	}
	return i
}

// skipStringAt skips the JSON string that starts at data[i], a quote, and
// returns the index past its closing quote. Raw control bytes and any escape
// but the eight short ones and \u with four hex digits are rejected.
func skipStringAt(data []byte, i int) (int, bool) {
	if i == len(data) || data[i] != '"' {
		return 0, false
	}
	return stringEnd(data, i+1)
}

// stringRule is how a string scan reads a body: run skips the bytes the rule
// keeps as they are, and keeps accepts a rune that runeAt returns.
type stringRule interface {
	run(data []byte, j int) int
	keeps(r rune) bool
}

// anyBody keeps every byte a body may hold and every well-formed escape, as
// json.Valid does.
type anyBody struct{}

func (anyBody) run(data []byte, j int) int { return bodyRun(data, j) }

func (anyBody) keeps(r rune) bool { return r != malformedRune }

// utf8Body keeps valid UTF-8 and every well-formed escape but a lone surrogate
// half, so it keeps only code points.
type utf8Body struct{}

func (utf8Body) run(data []byte, j int) int { return cleanRun(data, j) }

func (utf8Body) keeps(r rune) bool { return r >= 0 }

// bodyEnd returns the index past the closing quote of the string body that
// continues at data[j], read by the rule R.
func bodyEnd[R stringRule](data []byte, j int) (int, bool) {
	var rule R
	for {
		j = rule.run(data, j)
		switch {
		case j == len(data):
			return 0, false
		case data[j] == '"':
			return j + 1, true
		case data[j] != '\\':
			return 0, false
		}
		r, n := escapeAt(data, j)
		if !rule.keeps(r) {
			return 0, false
		}
		j += n
	}
}

// stringEnd returns the index past the closing quote of the string body that
// continues at data[j].
func stringEnd(data []byte, j int) (int, bool) {
	return bodyEnd[anyBody](data, j)
}

// closerOffset is how far each closer sits past its opener, '}' past '{' and ']'
// past '['.
const closerOffset = '}' - '{'

// closerOf returns '}' for '{' and ']' for '['.
func closerOf(c byte) byte {
	return c + closerOffset
}

// skipBraced skips the balanced pair that the opener data[i] starts and its closer
// ends. Only that pair is counted, so [{] passes, and each string inside must be
// well formed.
func skipBraced(data []byte, i int) (int, bool) {
	opener := data[i]
	closer := closerOf(opener)
	depth := 1
	for j := i + 1; ; {
		j = bracedRun(data, j, opener)
		if j == len(data) {
			return 0, false
		}
		switch data[j] {
		case '"':
			end, ok := skipStringAt(data, j)
			if !ok {
				return 0, false
			}
			j = end
			continue
		case opener:
			depth++
		case closer:
			depth--
		}
		j++
		if depth == 0 {
			return j, true
		}
	}
}

// bracedRun returns the index of the first quote, opener or closer of opener at
// or after data[j], or len(data).
func bracedRun(data []byte, j int, opener byte) int {
	open, shut := swarLo*uint64(opener), swarLo*uint64(closerOf(opener))
	for range (len(data) - j) / wordSize {
		if w := load64(data, j); zeroLanes(w^swarQuote)|zeroLanes(w^open)|zeroLanes(w^shut) != 0 {
			return bracedBytes(data, j, opener)
		}
		j += wordSize
	}
	return bracedBytes(data, j, opener)
}

// bracedBytes is bracedRun one byte at a time.
func bracedBytes(data []byte, j int, opener byte) int {
	closer := closerOf(opener)
	for j < len(data) && data[j] != '"' && data[j] != opener && data[j] != closer {
		j++
	}
	return j
}
