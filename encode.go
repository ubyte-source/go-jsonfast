package jsonfast

import (
	"cmp"
	"maps"
	"math/bits"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// AddValueElement writes v as the next element: string, bool, int, int64, uint64 and
// float64 as their Element writers, map[string]any by key, []any in order, others as
// text(v) reports, raw if IsNumber; null for the rest, for cycles and past MaxDepth.
func (b *Builder) AddValueElement(v any, text func(v any) (string, bool)) {
	b.sep()
	b.appendValue(v, text)
}

// AddValueField writes "name":v with name escaped: string, bool, int, int64, uint64
// and float64 as their Field writers, map[string]any by key, []any in order, others as
// text(v) reports, raw if IsNumber; null for the rest, for cycles and past MaxDepth.
func (b *Builder) AddValueField(name string, v any, text func(v any) (string, bool)) {
	b.fieldKey(name)
	b.appendValue(v, text)
}

// inlineValueKeys is how many keys of its open objects appendValue sorts on the
// goroutine stack before its stack of them moves to the heap.
const inlineValueKeys = 32

// containerID identifies a map by its pointer and a slice by its data pointer
// and length, as a cycle meets it again.
type containerID struct {
	ptr    uintptr
	length int
}

// encodeFrame is a container being written: its object, or else its array, its
// identity, where its keys start among the keys, its next key or element, and 1 +
// the index of the next older frame in its bucket, or 0.
type encodeFrame struct {
	object   map[string]any
	array    []any
	id       containerID
	firstKey int
	next     int
	older    int
}

// valueEncoder writes the Go values of appendValue into the Builder its methods
// take. The containers it is inside and their sorted keys sit on stacks that
// appendValue alone grows, so they stay on the goroutine stack and no nesting grows it.
type valueEncoder struct {
	text    func(v any) (string, bool)
	open    []encodeFrame
	keys    []string
	buckets []int // 1 + the index of the newest frame in each, once frames spill
}

// appendValue writes v, the value of the value writers, with no separator.
func (b *Builder) appendValue(v any, text func(v any) (string, bool)) {
	var (
		frames [inlineOpenValues]encodeFrame
		keys   [inlineValueKeys]string
	)
	e := valueEncoder{text: text, open: frames[:0], keys: keys[:0]}
	for more := true; more; v, more = e.next(b) {
		if f, open := e.value(b, v); open {
			e.keys = appendSortedKeys(e.keys, f.object)
			e.open = append(e.open, f)
			e.index()
		}
	}
}

// value writes v, or the opener of the container v and its frame to push.
func (e *valueEncoder) value(b *Builder, v any) (f encodeFrame, open bool) {
	switch x := v.(type) {
	case string:
		b.appendQuoted(bytesOf(x))
	case bool:
		b.buf = strconv.AppendBool(b.buf, x)
	case int:
		b.appendInt64(int64(x))
	case int64:
		b.appendInt64(x)
	case uint64:
		b.appendUint64(x)
	case float64:
		b.appendFloat64(x)
	case map[string]any:
		return e.enter(b, v, encodeFrame{object: x, firstKey: len(e.keys)})
	case []any:
		return e.enter(b, v, encodeFrame{array: x})
	default:
		e.other(b, v)
	}
	return f, false
}

// other writes the text that text reports for v, raw when IsNumber accepts it
// and quoted otherwise, or null when v is nil or text reports none.
func (e *valueEncoder) other(b *Builder, v any) {
	s, ok := "", false
	if v != nil && e.text != nil {
		s, ok = e.text(v)
	}
	switch {
	case !ok:
		b.buf = append(b.buf, litNull...)
	case IsNumber(s):
		b.buf = append(b.buf, s...)
	default:
		b.appendQuoted(bytesOf(s))
	}
}

// enter writes the opener of the container v and returns its frame f, or writes
// null when v is nil, already open on the path, or inside MaxDepth containers.
func (e *valueEncoder) enter(b *Builder, v any, f encodeFrame) (encodeFrame, bool) {
	f.id = containerID{ptr: reflect.ValueOf(v).Pointer(), length: len(f.array)}
	if f.id.ptr == 0 || len(e.open) == MaxDepth || e.onPath(f.id) {
		b.buf = append(b.buf, litNull...)
		return f, false
	}
	if f.object != nil {
		b.BeginObject()
	} else {
		b.BeginArray()
	}
	return f, true
}

// onPath reports whether the container id is open already: among the inline
// frames by a scan, and past them through its bucket.
func (e *valueEncoder) onPath(id containerID) bool {
	if e.buckets == nil {
		for _, f := range e.open {
			if f.id == id {
				return true
			}
		}
		return false
	}
	for at := e.buckets[e.bucket(id)]; at != 0; at = e.open[at-1].older {
		if e.open[at-1].id == id {
			return true
		}
	}
	return false
}

// index files the frame just pushed in its bucket once the frames spill, and
// refiles them all once fewer than bucketsPerFrame buckets per frame remain.
func (e *valueEncoder) index() {
	n := len(e.open)
	switch {
	case e.buckets == nil && n <= inlineOpenValues:
		return
	case n > len(e.buckets)/bucketsPerFrame:
		e.buckets = make([]int, bucketsGrowth*bucketsPerFrame*n)
		for k := range n - 1 {
			e.file(k)
		}
	}
	e.file(n - 1)
}

// bucketsPerFrame keeps the buckets at least twice as many as the frames they hold,
// so their chains stay short; a refile makes bucketsGrowth times as many as that, so
// it comes once each time the frames double.
const (
	bucketsPerFrame = 2
	bucketsGrowth   = 2
)

// file puts the frame at index k at the head of its bucket.
func (e *valueEncoder) file(k int) {
	at := e.bucket(e.open[k].id)
	e.open[k].older, e.buckets[at] = e.buckets[at], k+1
}

// fibonacciHash is 2^64 divided by the golden ratio, whose product spreads
// pointers over the high bits that bucket keeps.
const fibonacciHash = 0x9e3779b97f4a7c15

// bucket returns the index of the bucket of the container id: the high bits of
// its pointer times fibonacciHash, scaled to the number of buckets.
func (e *valueEncoder) bucket(id containerID) uint64 {
	index, _ := bits.Mul64(uint64(id.ptr)*fibonacciHash, uint64(len(e.buckets)))
	return index
}

// next closes the containers that hold nothing more and returns the next value
// to write, after its comma and its key; more is false once v is written.
func (e *valueEncoder) next(b *Builder) (v any, more bool) {
	for len(e.open) > 0 {
		if v, more = e.item(b, &e.open[len(e.open)-1]); more {
			return v, true
		}
		e.close(b)
	}
	return nil, false
}

// item writes the comma, and in an object the key, of the next value of f and
// returns that value; more is false when f holds no more.
func (e *valueEncoder) item(b *Builder, f *encodeFrame) (v any, more bool) {
	if f.object == nil {
		if f.next == len(f.array) {
			return nil, false
		}
		f.next++
		b.sep()
		return f.array[f.next-1], true
	}
	k := f.firstKey + f.next
	if k == len(e.keys) {
		return nil, false
	}
	f.next++
	b.fieldKey(e.keys[k])
	return f.object[e.keys[k]], true
}

// close writes the closer of the innermost container and leaves it.
func (e *valueEncoder) close(b *Builder) {
	f := &e.open[len(e.open)-1]
	if f.object != nil {
		e.keys = e.keys[:f.firstKey]
		b.EndObject()
	} else {
		b.EndArray()
	}
	if e.buckets != nil {
		e.buckets[e.bucket(f.id)] = f.older
	}
	e.open = e.open[:len(e.open)-1]
}

// appendSortedKeys appends the keys of m to dst in the order of their names as
// written, U+FFFD for each byte utf8 rejects; of keys written alike, only the
// greatest stays.
func appendSortedKeys(dst []string, m map[string]any) []string {
	start := len(dst)
	dst = slices.AppendSeq(slices.Grow(dst, len(m)), maps.Keys(m))
	keys := dst[start:]
	if !slices.ContainsFunc(keys, notUTF8) {
		slices.Sort(keys)
		return dst
	}
	slices.SortFunc(keys, compareKeys)
	return dst[:start+len(slices.CompactFunc(keys, writtenAlike))]
}

// notUTF8 reports whether s holds a byte utf8 rejects.
func notUTF8(s string) bool {
	return !utf8.ValidString(s)
}

// compareKeys orders keys by their names as written, and keys written alike
// from the greatest down.
func compareKeys(a, b string) int {
	return cmp.Or(compareWritten(a, b), strings.Compare(b, a))
}

// writtenAlike reports whether the keys a and b are written as one name.
func writtenAlike(a, b string) bool {
	return compareWritten(a, b) == 0
}

// compareWritten compares the names x and y by rune, each byte utf8 rejects read as
// U+FFFD. Equal bytes read alike, so it passes their common run at once and decodes
// runes only from the start of the one where the names part.
func compareWritten(x, y string) int {
	for {
		p := commonRunes(x, y)
		if x, y = x[p:], y[p:]; x == "" || y == "" {
			return cmp.Compare(len(x), len(y))
		}
		rx, sx := utf8.DecodeRuneInString(x)
		ry, sy := utf8.DecodeRuneInString(y)
		if rx != ry {
			return cmp.Compare(rx, ry)
		}
		x, y = x[sx:], y[sy:]
	}
}

// commonRunes returns the length of the longest common prefix of a and b that
// ends where a rune of each starts, or ends.
func commonRunes(a, b string) int {
	p := min(len(a), len(b))
	for i := range p {
		if a[i] != b[i] {
			p = i
			break
		}
	}
	for p > 0 && (inRune(a, p) || inRune(b, p)) {
		p--
	}
	return p
}

// inRune reports whether s[i] is a byte that continues a rune.
func inRune(s string, i int) bool {
	return i < len(s) && !utf8.RuneStart(s[i])
}
