package jsonfast

import (
	"bytes"
	"slices"
	"strconv"
)

// Builder appends JSON to a reusable byte slice; its zero value is ready to use and
// not safe for concurrent use. Field and Element writers put the comma a value needs
// before it, Field writers a name too, and BeginObject and BeginArray neither.
type Builder struct {
	buf     []byte
	needSep bool
}

// defaultCapacity is the capacity New gives a Builder when asked for none.
const defaultCapacity = 256

// New returns a Builder with the given initial capacity; a capacity below 1
// gives 256 bytes, and it panics if capacity is past what a byte slice can hold.
func New(capacity int) *Builder {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &Builder{buf: make([]byte, 0, capacity)}
}

// Reset empties the Builder and keeps its buffer.
func (b *Builder) Reset() {
	b.buf = b.buf[:0]
	b.needSep = false
}

// Bytes returns what the Builder holds. The slice aliases the buffer, so it
// is valid until the next write, Reset or Release.
func (b *Builder) Bytes() []byte {
	return b.buf
}

// Len returns the number of bytes the Builder holds.
func (b *Builder) Len() int {
	return len(b.buf)
}

// Grow makes room for n more bytes, so they need no further allocation; it panics,
// as slices.Grow does, if n is negative or the result is past what a byte slice can
// hold.
func (b *Builder) Grow(n int) {
	b.buf = slices.Grow(b.buf, n)
}

// BeginObject writes '{'. It writes no separator, so it composes after any
// key writer.
func (b *Builder) BeginObject() {
	b.buf = append(b.buf, '{')
	b.needSep = false
}

// BeginObjectField writes "name":{ with name escaped.
func (b *Builder) BeginObjectField(name string) {
	b.fieldKey(name)
	b.BeginObject()
}

// BeginObjectElement writes '{' as the next element, after a comma when an
// element precedes it.
func (b *Builder) BeginObjectElement() {
	b.sep()
	b.BeginObject()
}

// EndObject writes '}', which closes any object opener, so the next field or
// element gets its comma.
func (b *Builder) EndObject() {
	b.buf = append(b.buf, '}')
	b.needSep = true
}

// BeginArray writes '['. It writes no separator, so it composes after any key
// writer.
func (b *Builder) BeginArray() {
	b.buf = append(b.buf, '[')
	b.needSep = false
}

// BeginArrayField writes "name":[ with name escaped.
func (b *Builder) BeginArrayField(name string) {
	b.fieldKey(name)
	b.BeginArray()
}

// BeginArrayElement writes '[' as the next element, after a comma when an
// element precedes it.
func (b *Builder) BeginArrayElement() {
	b.sep()
	b.BeginArray()
}

// EndArray writes ']', which closes any array opener, so the next field or
// element gets its comma.
func (b *Builder) EndArray() {
	b.buf = append(b.buf, ']')
	b.needSep = true
}

// AddStringField writes "name":"v" with both strings escaped.
func (b *Builder) AddStringField(name, v string) {
	b.fieldKey(name)
	b.appendQuoted(bytesOf(v))
}

// AddStringBytesField is AddStringField for a value held in a byte slice.
func (b *Builder) AddStringBytesField(name string, v []byte) {
	b.fieldKey(name)
	b.appendQuoted(v)
}

// AddStringArrayField writes "name":["v1","v2",...] with every string escaped;
// nil or empty values write [].
func (b *Builder) AddStringArrayField(name string, values []string) {
	b.fieldKey(name)
	b.appendStringArray(values)
}

// AddIntField writes "name":v.
func (b *Builder) AddIntField(name string, v int) {
	b.fieldKey(name)
	b.appendInt64(int64(v))
}

// AddInt64Field writes "name":v.
func (b *Builder) AddInt64Field(name string, v int64) {
	b.fieldKey(name)
	b.appendInt64(v)
}

// AddUint64Field writes "name":v.
func (b *Builder) AddUint64Field(name string, v uint64) {
	b.fieldKey(name)
	b.appendUint64(v)
}

// AddFloat64Field writes "name":v, with v as encoding/json writes it and
// null for NaN and ±Inf.
func (b *Builder) AddFloat64Field(name string, v float64) {
	b.fieldKey(name)
	b.appendFloat64(v)
}

// AddBoolField writes "name":true or "name":false.
func (b *Builder) AddBoolField(name string, v bool) {
	b.fieldKey(name)
	b.buf = strconv.AppendBool(b.buf, v)
}

// AddNullField writes "name":null.
func (b *Builder) AddNullField(name string) {
	b.fieldKey(name)
	b.buf = append(b.buf, litNull...)
}

// AddRawJSONField writes "name":raw with raw as it is, one valid JSON value or empty;
// an empty raw leaves the value to the next writer, which writes no separator then:
// BeginObject, BeginArray or a raw appender.
func (b *Builder) AddRawJSONField(name string, raw []byte) {
	b.fieldKey(name)
	b.buf = append(b.buf, raw...)
}

// AddRawBytesField is AddRawJSONField with name written as it is, the body of a JSON
// string.
func (b *Builder) AddRawBytesField(name, raw []byte) {
	b.sep()
	b.buf = append(b.buf, '"')
	b.buf = append(b.buf, name...)
	b.buf = append(b.buf, '"', ':')
	b.buf = append(b.buf, raw...)
}

// AddStringElement writes "v" as the next element, with v escaped, after a
// comma when an element precedes it.
func (b *Builder) AddStringElement(v string) {
	b.sep()
	b.appendQuoted(bytesOf(v))
}

// AddStringBytesElement is AddStringElement for a value held in a byte slice.
func (b *Builder) AddStringBytesElement(v []byte) {
	b.sep()
	b.appendQuoted(v)
}

// AddStringArrayElement writes ["v1","v2",...] as the next element, every string
// escaped; nil or empty values write [].
func (b *Builder) AddStringArrayElement(values []string) {
	b.sep()
	b.appendStringArray(values)
}

// AddIntElement writes v as the next element.
func (b *Builder) AddIntElement(v int) {
	b.sep()
	b.appendInt64(int64(v))
}

// AddInt64Element writes v as the next element.
func (b *Builder) AddInt64Element(v int64) {
	b.sep()
	b.appendInt64(v)
}

// AddUint64Element writes v as the next element.
func (b *Builder) AddUint64Element(v uint64) {
	b.sep()
	b.appendUint64(v)
}

// AddFloat64Element writes v as the next element, as encoding/json writes it, and
// null for NaN and ±Inf.
func (b *Builder) AddFloat64Element(v float64) {
	b.sep()
	b.appendFloat64(v)
}

// AddBoolElement writes true or false as the next element.
func (b *Builder) AddBoolElement(v bool) {
	b.sep()
	b.buf = strconv.AppendBool(b.buf, v)
}

// AddNullElement writes null as the next element.
func (b *Builder) AddNullElement() {
	b.sep()
	b.buf = append(b.buf, litNull...)
}

// AddRawJSONElement writes raw as it is as the next element, one valid JSON value or
// empty; an empty raw writes only the comma and leaves the element to the next writer.
func (b *Builder) AddRawJSONElement(raw []byte) {
	b.sep()
	b.buf = append(b.buf, raw...)
}

// AddRawMembers writes the members of the JSON object raw as they are, after a comma
// when a field precedes them; an object with no member writes nothing, and raw must
// be one valid JSON object.
func (b *Builder) AddRawMembers(raw []byte) {
	opener, closer := bytes.IndexByte(raw, '{'), bytes.LastIndexByte(raw, '}')
	members := TrimWS(raw[opener+1 : max(closer, opener+1)])
	if len(members) == 0 {
		return
	}
	b.sep()
	b.buf = append(b.buf, members...)
}

// AppendRaw appends p as it is.
func (b *Builder) AppendRaw(p []byte) {
	b.buf = append(b.buf, p...)
}

// AppendRawString appends s as it is.
func (b *Builder) AppendRawString(s string) {
	b.buf = append(b.buf, s...)
}

// sep writes the comma between this field or element and the one before it,
// when there is one.
func (b *Builder) sep() {
	if b.needSep {
		b.buf = append(b.buf, ',')
	}
	b.needSep = true
}

// fieldKey writes the separator and "name": with name escaped.
func (b *Builder) fieldKey(name string) {
	b.sep()
	b.appendQuoted(bytesOf(name))
	b.buf = append(b.buf, ':')
}

// appendQuoted writes "p" with p escaped.
func (b *Builder) appendQuoted(p []byte) {
	b.buf = append(b.buf, '"')
	b.AppendEscaped(p)
	b.buf = append(b.buf, '"')
}

// appendStringArray writes ["v1","v2",...] with every string escaped.
func (b *Builder) appendStringArray(values []string) {
	b.BeginArray()
	for _, v := range values {
		b.AddStringElement(v)
	}
	b.EndArray()
}
