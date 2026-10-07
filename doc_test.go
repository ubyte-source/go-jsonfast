package jsonfast

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Literals the test files share.
const (
	emptyObject     = "{}"
	emptyArray      = "[]"
	oneArray        = "[1]"
	twoArray        = "[1,2]"
	arrayGap        = "[1 2]"
	arrayTrail      = "[1,]"
	objectA1        = `{"a":1}`
	objectTrail     = `{"a":1,}`
	blank           = "   "
	plainText       = "plain"
	cafe            = "café"
	quotedHello     = `"hello"`
	quotedEmpty     = `""`
	loneHigh        = `"\ud800"`
	twoReplacements = "��"
	repeatedA       = `{"a":1,"a":2}`
)

// warmRuns is how many times assertAllocs runs f before it counts, which
// fills the pools and caches f touches.
const warmRuns = 4

// countRuns is how many runs assertAllocs averages.
const countRuns = 100

// repeats is how many times a test repeats what depends on chance, such as a
// pool dropping an item or the order of a map.
const repeats = 64

// Load of the concurrency tests: goroutines, and pool round trips of each.
const (
	workers    = 8
	roundTrips = 1000
)

// longRun is the length of a run that spans many SWAR words.
const longRun = 256

// hugeDigits is a length at which a copy of the input stands out in an
// allocation count.
const hugeDigits = 1 << 20

// replacement is U+FFFD, what an invalid byte or lone surrogate decodes to.
const replacement = "\uFFFD"

// A syslog record that the Builder benchmarks write field by field.
const (
	logMessage  = "User authentication failed for admin from 192.168.1.100 port 22 ssh2"
	logHost     = "webserver-prod-01"
	logSeverity = 4
	logFacility = 10
	logApp      = "sshd"
	logProc     = "28374"
	logMsgID    = "AUTH_FAIL"
	logVersion  = 1
	logSource   = "192.168.1.100"
)

// Radixes and sizes of the strconv calls that serve as oracles.
const (
	decimalRadix = 10
	hexRadix     = 16
	intBits      = 64
)

// validityCase is a text and whether it is one JSON value.
type validityCase struct {
	in    string
	valid bool
}

// validityCases returns texts paired with their validity; encoding/json
// agrees with every row.
func validityCases() []validityCase {
	return []validityCase{
		{emptyObject, true}, {emptyArray, true}, {` { } `, true}, {"\t\r\n[\t\r\n]\t\r\n", true},
		{`0`, true}, {`-0.5e+10`, true}, {`"s"`, true}, {litTrue, true}, {litFalse, true},
		{litNull, true}, {`{"a":1,"b":[true,false,null,{"c":"d"}],"e":{}}`, true},
		{`[1,"two",[3],{"4":5}]`, true}, {`[{"a":{}},[1],{"b":[]}]`, true}, {`[{"a":1},[[],1]]`, true},
		{`{"a" : 1 , "b" : [ 2 ] }`, true}, {repeatedA, true}, {"\"\x7f\"", true},
		{"\"\xff\xfe\"", true}, {loneHigh, true}, {`{"\udc00":"\ud800x"}`, true},
		{`"\u00e9\/\b\f\n\r\t\\\"\u0000"`, true}, {`"\u12"`, false}, {`"\u123`, false}, {`"\ud800\u12"`, false},
		{``, false}, {blank, false}, {`{`, false}, {`[`, false},
		{`}`, false}, {`]`, false}, {`[}`, false}, {`{]`, false}, {`[1}`, false},
		{`{"a":1]`, false}, {`[[]`, false}, {`[]]`, false}, {`{}{}`, false}, {`[] []`, false},
		{`1 2`, false}, {`"a" "b"`, false}, {`[1,]`, false}, {`[,1]`, false}, {`[1 2]`, false},
		{`{,}`, false}, {objectTrail, false}, {`{"a" 1}`, false}, {`{"a"}`, false},
		{`{"a":}`, false}, {`{"a":1 "b":2}`, false}, {`{1:2}`, false}, {`{a:1}`, false},
		{`{"a"::1}`, false}, {`{"a":1:2}`, false}, {`[01]`, false}, {`[1.]`, false},
		{`[.5]`, false}, {`[+1]`, false}, {`[1e]`, false}, {`[-]`, false}, {`[tru]`, false},
		{`[truex]`, false}, {`[nul]`, false}, {`[NaN]`, false}, {`["\x"]`, false},
		{`["\u12g4"]`, false}, {`["\u00"]`, false}, {`["\`, false}, {`["abc`, false},
		{"[\"a\x01\"]", false}, {"[\"a\n\"]", false}, {"[\xff]", false}, {"\xef\xbb\xbf{}", false},
		{"{}\x00", false}, {`{"a";1}`, false}, {"[\"a\t]", false}, {"\"\t", false},
	}
}

// inlineLevels is how many nesting levels the validator holds inline.
const inlineLevels = 256

// documentedNameSets is how many name sets of open objects FlattenObject,
// IterateDocument and WalkTokens keep inline, as their documentation states.
const documentedNameSets = 2

// siblings returns an object whose one member holds n copies of object in an array,
// so a walk closes n objects but holds one at a time.
func siblings(n int, object string) []byte {
	return []byte(`{"a":[` + strings.Repeat(object+",", n-1) + object + `]}`)
}

// Sibling counts of the tests of walks that close many objects one at a time:
// someSiblings escaped names fit the first chunk of an arena, and manySiblings
// outgrow it.
const (
	someSiblings = 1 << 6
	manySiblings = 1 << 14
)

// leveled returns a text nested exactly levels deep whose level i is an
// object when objectAt(i) holds and an array otherwise.
func leveled(levels int, objectAt func(int) bool) string {
	var head, tail strings.Builder
	for i := range levels {
		if objectAt(i) {
			head.WriteString(`{"k":`)
			tail.WriteString("}")
		} else {
			head.WriteString("[")
			tail.WriteString("]")
		}
	}
	closers := []byte(tail.String())
	slices.Reverse(closers)
	return head.String() + "0" + string(closers)
}

// validityDocument is a realistic request body for the validator benchmarks.
const validityDocument = `{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"search",` +
	`"arguments":{"query":"failed login for \"admin\"\nfrom 10.0.0.1","limit":50,` +
	`"filters":[{"field":"severity","op":"gte","value":4},{"field":"host","op":"eq",` +
	`"value":"fw01"}],"since":"2024-01-15T12:30:45Z","include_raw":false,"score":0.75}}}`

// utf8Cases returns texts paired with whether ValidUTF8 accepts them.
func utf8Cases() []validityCase {
	return []validityCase{
		{`{"caf\u00e9":"\ud83d\ude80 🚀"}`, true},
		{"[\"\xef\xbf\xbd\",\"\\ufffd\"]", true},
		{"[\"\\n\xef\xbf\xbd\"]", true},
		{"{\"caf\xc3\xa9\\t\":\"\\u00e9\xef\xbf\xbd\"}", true},
		{`"\udbff\udfff"`, true},
		{"\"\x7f\"", true},
		{"[\"\xff\"]", false},
		{"{\"\xff\":1}", false},
		{"\"\xf0\x9f\"", false},
		{"\"\xc0\x80\"", false},
		{"\"\xed\xa0\x80\"", false},
		{"\"\xf4\x90\x80\x80\"", false},
		{`["\ud800"]`, false},
		{`{"\udc00":1}`, false},
		{`"\ud800\u0041"`, false},
		{`"\ud800\ud800\udc00"`, false},
		{`"\udc00\ud800"`, false},
		{`["\q"]`, false},
		{`{"b":2,}`, false},
		{"\xff", false},
	}
}

// rawEqual reports whether two raw values hold the same bytes.
func rawEqual(a, b json.RawMessage) bool {
	return bytes.Equal(a, b)
}

// errStop is a callback error that the walks must return unchanged.
var errStop = errors.New("stop")

// raceEnabled reports whether the test binary runs the race detector.
func raceEnabled() bool {
	info, _ := debug.ReadBuildInfo()
	return info != nil && slices.Contains(info.Settings, debug.BuildSetting{Key: "-race", Value: "true"})
}

// assertAllocs fails t unless f allocates want times per run, after a warm-up
// that fills pools and caches. The race detector changes allocation counts
// and drops pooled items, so under it f runs once, unchecked.
func assertAllocs(t *testing.T, want float64, f func()) {
	t.Helper()
	if raceEnabled() {
		f()
		return
	}
	for range warmRuns {
		f()
	}
	if got := testing.AllocsPerRun(countRuns, f); got != want {
		t.Errorf("allocs per run = %.0f, want %.0f", got, want)
	}
}

// Bounds of assertCost: a linear walk over costScale times the input may cost
// costMargin*costScale times as much, in time and in allocated bytes, far below the
// costScale*costScale of a quadratic walk.
const (
	costScale    = 8
	costMargin   = 3
	costTrials   = 5
	costAttempts = 3
)

// assertCost fails t unless, in one of costAttempts measures, large takes at most
// n = costMargin*scale times as long as small and allocates at most n*(b+1) bytes,
// b being small's; -race only runs both.
func assertCost(t *testing.T, small, large func(), scale int) {
	t.Helper()
	if raceEnabled() {
		small()
		large()
		return
	}
	bound := float64(costMargin * scale)
	var got string
	for range costAttempts {
		smallTime, smallBytes := costOf(small)
		largeTime, largeBytes := costOf(large)
		if largeTime <= time.Duration(bound)*smallTime && float64(largeBytes) <= bound*float64(smallBytes+1) {
			return
		}
		got = fmt.Sprintf("%v and %d bytes, against %v and %d", largeTime, largeBytes, smallTime, smallBytes)
	}
	t.Errorf("the larger run took %s, want at most %g times the smaller (PERF-1.2, PERF-3.1)", got, bound)
}

// decidedSpaces is how much whitespace pads the documents of the tests whose depth
// bound decides the result, which a walk answers without reading them.
const decidedSpaces = 1 << 20

// costOf returns the time of the fastest of costTrials runs of f and the bytes
// that run allocated.
func costOf(f func()) (fastest time.Duration, allocated uint64) {
	var before, after runtime.MemStats
	for trial := range costTrials {
		runtime.ReadMemStats(&before)
		start := time.Now()
		f()
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		if trial == 0 || elapsed < fastest {
			fastest, allocated = elapsed, after.TotalAlloc-before.TotalAlloc
		}
	}
	return fastest, allocated
}

// Shape of levelsDocument: the depth of the smaller document, and the bytes of the
// string body that each level holds.
const (
	costLevels = 8
	levelText  = 4 << 10
)

// levelBody returns a string body of levelText bytes that ends in an escape.
func levelBody() string {
	return strings.Repeat("x", levelText-len(`\n`)) + `\n`
}

// levelsDocument returns an object nested depth levels deep, each level a member
// "s<level>" holding a string body of levelText bytes that ends in an escape, and a
// member "n<level>" holding the next level; no two names repeat.
func levelsDocument(depth int) []byte {
	var doc strings.Builder
	for level := range depth {
		fmt.Fprintf(&doc, `{"s%d":"%s","n%d":`, level, levelBody(), level)
	}
	doc.WriteString("0")
	doc.WriteString(strings.Repeat("}", depth))
	return []byte(doc.String())
}

// repeatedName opens an object whose second member repeats the name of the first.
const repeatedName = `{"a":1,"a":1`

// tailMembers is how many members past the repeat the long documents of the
// repeated-name tests hold.
const tailMembers = 1000

// repeatedTail returns the rest of an object that repeatedName opens: n members
// with numbers, arrays, nested objects and escapes, then the closer.
func repeatedTail(n int) string {
	return strings.Repeat(`,"m":[1,{"s":"x\ny"}]`, n) + "}"
}

// benchWrite fails b unless write, on an empty Builder, writes want; then it
// times write on one Builder, reset before each run, and reports the bytes
// one run writes.
func benchWrite(b *testing.B, want string, write func(builder *Builder)) {
	b.Helper()
	builder := New(0)
	write(builder)
	if got := string(builder.Bytes()); got != want {
		b.Fatalf("wrote %s, want %s", got, want)
	}
	b.ReportAllocs()
	b.SetBytes(int64(builder.Len()))
	for b.Loop() {
		builder.Reset()
		write(builder)
	}
}

// syslogRecord returns the record the syslog benchmarks write, as
// encoding/json writes it.
func syslogRecord(tb testing.TB) string {
	tb.Helper()
	out, err := json.Marshal(struct {
		Message  string `json:"message"`
		Hostname string `json:"hostname"`
		Severity int    `json:"severity"`
		Facility int    `json:"facility"`
		AppName  string `json:"app_name"`
		ProcID   string `json:"proc_id"`
		MsgID    string `json:"msg_id"`
		Version  int    `json:"version"`
		Source   string `json:"source"`
	}{logMessage, logHost, logSeverity, logFacility, logApp, logProc, logMsgID, logVersion, logSource})
	noError(tb, err)
	return string(out)
}

// stdJSON returns v as encoding/json writes it with SetEscapeHTML(false).
func stdJSON(tb testing.TB, v any) string {
	tb.Helper()
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	noError(tb, enc.Encode(v))
	return strings.TrimSuffix(out.String(), "\n")
}

// encodingJSONForm returns the Builder's output out with U+2028 and U+2029
// escaped, the one difference from what stdJSON writes.
func encodingJSONForm(out []byte) string {
	return strings.NewReplacer("\u2028", `\u2028`, "\u2029", `\u2029`).Replace(string(out))
}

func panics(f func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	f()
	return false
}

// tooLarge lists the sizes past what a byte slice can hold: math.MaxInt where int
// is wider than 32 bits, and none on a 32-bit target, whose int stays below them.
func tooLarge() []int {
	if math.MaxInt > math.MaxInt32 {
		return []int{math.MaxInt}
	}
	return nil
}

// noError fails tb when err is not nil.
func noError(tb testing.TB, err error) {
	tb.Helper()
	if err != nil {
		tb.Fatalf("error = %v, want nil", err)
	}
}

// is reports whether err is want itself, not a wrapper of it; the walks
// return their sentinels and callback errors bare.
func is(err, want error) bool {
	if err == nil || want == nil {
		return err == nil && want == nil
	}
	return errors.Is(err, want) && errors.Is(want, err)
}

// jsonSpace holds the four JSON whitespace bytes, the cutset of the bytes.Trim
// oracles.
const jsonSpace = " \t\r\n"

// quote is the byte that opens and closes a JSON string.
const quote = `"`

func quoted(body string) string {
	return quote + body + quote
}

// instant parses s, a time in the form time.RFC3339Nano reads.
func instant(tb testing.TB, s string) time.Time {
	tb.Helper()
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		tb.Fatalf("time.Parse(%q) = %v, want nil", s, err)
	}
	return t
}

// nest returns depth copies of open, then inner, then depth copies of shut.
func nest(open, inner, shut string, depth int) string {
	return strings.Repeat(open, depth) + inner + strings.Repeat(shut, depth)
}

// arrays returns depth empty arrays nested in one another.
func arrays(depth int) string {
	return inArrays("", depth)
}

func inArrays(inner string, depth int) string {
	return nest("[", inner, "]", depth)
}

// aliases reports whether the bytes of s start at within[at] in memory.
func aliases[T Text](s T, within string, at int) bool {
	b := bytesOf(s)
	return len(b) > 0 && &b[0] == &bytesOf(within)[at]
}

// refusedLate returns a string token whose one invalid byte sits between two clean
// runs of longRun bytes.
func refusedLate() string {
	return quoted(strings.Repeat("a", longRun) + "\xff" + strings.Repeat("b", longRun))
}

// sameScalar reports whether got, ok is want, wantOK, and got the zero value when
// ok is false.
func sameScalar[V comparable](got V, ok bool, want V, wantOK bool) bool {
	var zero V
	return ok == wantOK && (ok && got == want || !ok && got == zero)
}

// spillAllocs is how many allocations the map of a name set takes once its names
// pass the ones it holds inline.
const spillAllocs = 4

// capped reports whether the view v cannot grow in place.
func capped(v []byte) bool {
	return cap(v) == len(v)
}

// decoderMembers lists the names and raw values of the object in data as
// json.Decoder reads them.
func decoderMembers(data []byte) (names []string, values []json.RawMessage, err error) {
	names, values = []string{}, []json.RawMessage{}
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err = dec.Token(); err != nil {
		return nil, nil, fmt.Errorf("open: %w", err)
	}
	for dec.More() {
		var tok json.Token
		if tok, err = dec.Token(); err != nil {
			return nil, nil, fmt.Errorf("name: %w", err)
		}
		v := json.RawMessage{}
		if err = dec.Decode(&v); err != nil {
			return nil, nil, fmt.Errorf("value: %w", err)
		}
		names, values = append(names, fmt.Sprint(tok)), append(values, v)
	}
	return names, values, nil
}

// marshalDecoded returns what encoding/json decodes the marshaled s to.
func marshalDecoded(s string) (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	var out string
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	return out, nil
}

// nesting returns how many arrays and objects of data, which json.Valid
// accepts, nest at the deepest point, as json.Decoder reads them.
func nesting(data []byte) (int, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	depth, deepest := 0, 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return deepest, nil
		}
		if err != nil {
			return 0, fmt.Errorf("token: %w", err)
		}
		switch tok {
		case json.Delim('['), json.Delim('{'):
			depth++
			deepest = max(deepest, depth)
		case json.Delim(']'), json.Delim('}'):
			depth--
		}
	}
}

// The tokens of the shallow walk grammar, read with no function of the package: a
// string token, and a number with its fraction and exponent groups.
var (
	refStringToken = regexp.MustCompile(`^"(?:[^"\\\x00-\x1f]|\\["\\/bfnrt]|\\u[0-9a-fA-F]{4})*"`)
	refNumberToken = regexp.MustCompile(`^-?(?:0|[1-9]\d*)(?P<fraction>\.\d+)?(?P<exponent>[eE][+-]?\d+)?`)
)

// refSpace returns the index of the first byte at or after data[i] that is not
// JSON whitespace.
func refSpace(data []byte, i int) int {
	for i < len(data) && strings.IndexByte(jsonSpace, data[i]) >= 0 {
		i++
	}
	return i
}

// refAt reports whether data[i] exists and is c.
func refAt(data []byte, i int, c byte) bool {
	return i < len(data) && data[i] == c
}

// refCloser returns the closer of the opener '{' or '['.
func refCloser(opener byte) byte {
	if opener == '{' {
		return '}'
	}
	return ']'
}

// refString returns the index past the string token at data[i], or -1.
func refString(data []byte, i int) int {
	if m := refStringToken.Find(data[i:]); m != nil {
		return i + len(m)
	}
	return -1
}

// refNumber returns the index past the number at data[i], or -1: a point or an
// exponent mark right after the part that may end in one starts no digit.
func refNumber(data []byte, i int) int {
	m := refNumberToken.FindSubmatchIndex(data[i:])
	if m == nil {
		return -1
	}
	end := i + m[1]
	noFraction := m[2*refNumberToken.SubexpIndex("fraction")] < 0
	noExponent := m[2*refNumberToken.SubexpIndex("exponent")] < 0
	if refAt(data, end, 'e') || refAt(data, end, 'E') {
		if noExponent {
			return -1
		}
	}
	if refAt(data, end, '.') && noFraction && noExponent {
		return -1
	}
	return end
}

// refBraced returns the index past the pair that the opener data[i] starts and
// its closer ends, counting only that pair outside strings, or -1.
func refBraced(data []byte, i int) int {
	opener, depth := data[i], 0
	closer := refCloser(opener)
	for j := i; j < len(data); {
		switch data[j] {
		case '"':
			if j = refString(data, j); j < 0 {
				return -1
			}
			continue
		case opener:
			depth++
		case closer:
			depth--
		}
		j++
		if depth == 0 {
			return j
		}
	}
	return -1
}

// refValue returns the index past the value at data[i], or -1: a string, a number
// or a literal read in full, or an array or object by refBraced.
func refValue(data []byte, i int) int {
	if i == len(data) {
		return -1
	}
	switch data[i] {
	case '"':
		return refString(data, i)
	case '{', '[':
		return refBraced(data, i)
	}
	for _, lit := range []string{litTrue, litFalse, litNull} {
		if bytes.HasPrefix(data[i:], []byte(lit)) {
			return i + len(lit)
		}
	}
	return refNumber(data, i)
}

// opens reports whether the first byte of data past JSON whitespace is opener.
func opens(data []byte, opener byte) bool {
	trimmed := bytes.TrimLeft(data, jsonSpace)
	return len(trimmed) > 0 && trimmed[0] == opener
}

// numberOf is the number conversion of the tests: json.Number of the raw text,
// as encoding/json decodes with UseNumber.
func numberOf[T Text](raw T) any { return json.Number(raw) }

// stdValue decodes the next value of dec as encoding/json does into an any
// with UseNumber, and reports whether an object in it repeats a member name.
func stdValue(dec *json.Decoder) (v any, repeated bool, err error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, false, fmt.Errorf("token: %w", err)
	}
	switch tok {
	case json.Delim('['):
		return stdArray(dec)
	case json.Delim('{'):
		return stdObject(dec)
	}
	return tok, false, nil
}

// stdArray decodes the rest of the array stdValue opened.
func stdArray(dec *json.Decoder) (v any, repeated bool, err error) {
	a := []any{}
	for dec.More() {
		var (
			elem any
			r    bool
		)
		if elem, r, err = stdValue(dec); err != nil {
			return nil, false, err
		}
		a, repeated = append(a, elem), repeated || r
	}
	return a, repeated, stdClose(dec)
}

// stdObject decodes the rest of the object stdValue opened.
func stdObject(dec *json.Decoder) (v any, repeated bool, err error) {
	m := map[string]any{}
	for dec.More() {
		name, nameErr := dec.Token()
		if nameErr != nil {
			return nil, false, fmt.Errorf("name: %w", nameErr)
		}
		var (
			decoded any
			r       bool
		)
		if decoded, r, err = stdValue(dec); err != nil {
			return nil, false, err
		}
		_, dup := m[fmt.Sprint(name)]
		m[fmt.Sprint(name)], repeated = decoded, repeated || r || dup
	}
	return m, repeated, stdClose(dec)
}

// stdClose reads the closer of the container stdValue decodes.
func stdClose(dec *json.Decoder) error {
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("closer: %w", err)
	}
	return nil
}

// strictlyRead fails tb unless every strict reader accepts out as one JSON value:
// encoding/json with no repeated name and no string that decodes to a substitute,
// ValidUTF8, DecodeValue and, for an object, IterateMembers.
func strictlyRead(tb testing.TB, out []byte) {
	tb.Helper()
	if !strictJSON(out) {
		tb.Fatalf("encoding/json reads %q with a fault, a repeated name or a substitute, want none", out)
	}
	if !ValidUTF8(out, len(out)) {
		tb.Fatalf("ValidUTF8(%q) = false, want true", out)
	}
	if _, err := DecodeValue(out, len(out), numberOf[[]byte]); err != nil {
		tb.Fatalf("DecodeValue(%q) = %v, want nil", out, err)
	}
	if opens(out, '{') {
		if err := IterateMembers(out, func(_, _ []byte) error { return nil }); err != nil {
			tb.Fatalf("IterateMembers(%q) = %v, want nil", out, err)
		}
	}
}

// strictJSON reports whether encoding/json reads data as one value with no
// repeated member name and no string that decodes to a substitute.
func strictJSON(data []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	_, repeated, err := stdValue(dec)
	return err == nil && !repeated && jsonUTF8(data)
}

// flatStack is the goroutine stack that the tests of the iterative walks allow,
// far below what a frame per level of their documents would take.
const flatStack = 1 << 20

// seedNesting is how deep the nested seed of FuzzDecodeValue goes.
const seedNesting = 4

// documentedOpenValues is how many open containers DecodeValue and the value
// writers keep inline, as their documentation states.
const documentedOpenValues = 16

// valueDocument is the document of the DecodeValue and value writer benchmarks.
const valueDocument = `{"id":"abc","name":"switch-1","page":2,"verbose":true,"tags":["a","b"],"filter":{"site":"x"}}`

// documentedRoom is the most room a new arena chunk of a walk takes for the rest of
// its document, as the documentation states: 4 KiB.
const documentedRoom = 4 << 10

// escapedChunks is how many arena chunks the 32 escaped tokens of escapedTokens
// take in one walk: one, as their document is shorter than 4 KiB.
const escapedChunks = 1

// escapedTokens returns the 32 string tokens "k\u00a0" to "k\u00bf", nine bytes
// each and each to decode.
func escapedTokens() []string {
	tokens := make([]string, inlineNames)
	for i := range tokens {
		tokens[i] = fmt.Sprintf(`"k\u00%02x"`, 0xa0+i)
	}
	return tokens
}

// escapedArray returns the array of the escapedTokens.
func escapedArray() []byte {
	return []byte("[" + strings.Join(escapedTokens(), ",") + "]")
}

// escapedObject returns the object whose names are the escapedTokens, each
// with the value 0.
func escapedObject() []byte {
	return []byte("{" + strings.Join(escapedTokens(), ":0,") + ":0}")
}

// replacementFree reports whether no string of the valid JSON text doc holds
// invalid UTF-8 or a lone surrogate escape.
func replacementFree(doc []byte) bool {
	inString := false
	for i := 0; i < len(doc); i++ {
		c := doc[i]
		if !inString || c == '"' {
			inString = inString != (c == '"')
			continue
		}
		n, ok := 1, true
		switch {
		case c == '\\' && doc[i+1] != 'u':
			n = 2
		case c == '\\':
			n, ok = surrogateEscapeLen(doc, i)
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRune(doc[i:])
			n, ok = size, r != utf8.RuneError || size > 1
		}
		if !ok {
			return false
		}
		i += n - 1
	}
	return true
}

// Bounds of the UTF-16 surrogate halves, the high ones first.
const (
	firstSurrogate = 0xD800
	firstLowHalf   = 0xDC00
	lastSurrogate  = 0xDFFF
)

// surrogateEscapeLen returns the length of the \u escape at doc[i], that of two
// for a surrogate pair, and false for a lone surrogate half.
func surrogateEscapeLen(doc []byte, i int) (int, bool) {
	const escape = len(`\u0000`)
	hi, err := strconv.ParseInt(string(doc[i+len(`\u`):i+escape]), hexRadix, intBits)
	switch {
	case err != nil:
		return 0, false
	case hi < firstSurrogate || hi > lastSurrogate:
		return escape, true
	case hi >= firstLowHalf || i+2*escape > len(doc) || string(doc[i+escape:i+escape+len(`\u`)]) != `\u`:
		return 0, false
	}
	lo, err := strconv.ParseInt(string(doc[i+escape+len(`\u`):i+2*escape]), hexRadix, intBits)
	return 2 * escape, err == nil && lo >= firstLowHalf && lo <= lastSurrogate
}

// jsonUTF8 reports whether encoding/json accepts data and no string of it
// decodes to U+FFFD: what ValidUTF8 reports, found independently of it.
func jsonUTF8(data []byte) bool {
	return json.Valid(data) && replacementFree(data)
}

// firstMatch returns the index of the first byte of data at or after j that
// keep rejects, or len(data).
func firstMatch(data []byte, j int, keep func(byte) bool) int {
	for j < len(data) && keep(data[j]) {
		j++
	}
	return j
}

// runInputs returns nil, plain texts of every length up to four words, and each
// of them with one byte of stops at each position. A text shares its buffer with
// the plain text that follows it, which a read past its length would take in.
func runInputs(stops string) [][]byte {
	text := strings.Repeat("abcdefgh", wordSize/2)
	out := [][]byte{nil}
	for n := range len(text) + 1 {
		out = append(out, []byte(text)[:n])
		for pos := range n * len(stops) {
			data := []byte(text)
			data[pos/len(stops)] = stops[pos%len(stops)]
			out = append(out, data[:n])
		}
	}
	return out
}
