package jsonfast

import (
	"strconv"
	"time"
)

// FieldKey is the precomputed key of a field, its name escaped once. Only
// NewFieldKey builds one; the Builder's FieldKey writers panic on the zero
// FieldKey.
type FieldKey struct {
	prefix string // ,"name":
}

// NewFieldKey returns the FieldKey of name, escaped as the Field writers escape it,
// so every FieldKey writer writes what its Field twin writes for name.
func NewFieldKey(name string) FieldKey {
	return FieldKey{prefix: `,"` + EscapeString(name) + `":`}
}

// BeginObjectFieldKey writes the key of k and '{'.
func (b *Builder) BeginObjectFieldKey(k FieldKey) {
	b.precomputedKey(k)
	b.BeginObject()
}

// BeginArrayFieldKey writes the key of k and '['.
func (b *Builder) BeginArrayFieldKey(k FieldKey) {
	b.precomputedKey(k)
	b.BeginArray()
}

// AddStringFieldKey is AddStringField with a precomputed key.
func (b *Builder) AddStringFieldKey(k FieldKey, v string) {
	b.precomputedKey(k)
	b.appendQuoted(bytesOf(v))
}

// AddStringBytesFieldKey is AddStringBytesField with a precomputed key.
func (b *Builder) AddStringBytesFieldKey(k FieldKey, v []byte) {
	b.precomputedKey(k)
	b.appendQuoted(v)
}

// AddStringArrayFieldKey is AddStringArrayField with a precomputed key.
func (b *Builder) AddStringArrayFieldKey(k FieldKey, values []string) {
	b.precomputedKey(k)
	b.appendStringArray(values)
}

// AddIntFieldKey is AddIntField with a precomputed key.
func (b *Builder) AddIntFieldKey(k FieldKey, v int) {
	b.precomputedKey(k)
	b.appendInt64(int64(v))
}

// AddInt64FieldKey is AddInt64Field with a precomputed key.
func (b *Builder) AddInt64FieldKey(k FieldKey, v int64) {
	b.precomputedKey(k)
	b.appendInt64(v)
}

// AddUint64FieldKey is AddUint64Field with a precomputed key.
func (b *Builder) AddUint64FieldKey(k FieldKey, v uint64) {
	b.precomputedKey(k)
	b.appendUint64(v)
}

// AddFloat64FieldKey is AddFloat64Field with a precomputed key.
func (b *Builder) AddFloat64FieldKey(k FieldKey, v float64) {
	b.precomputedKey(k)
	b.appendFloat64(v)
}

// AddBoolFieldKey is AddBoolField with a precomputed key.
func (b *Builder) AddBoolFieldKey(k FieldKey, v bool) {
	b.precomputedKey(k)
	b.buf = strconv.AppendBool(b.buf, v)
}

// AddNullFieldKey is AddNullField with a precomputed key.
func (b *Builder) AddNullFieldKey(k FieldKey) {
	b.precomputedKey(k)
	b.buf = append(b.buf, litNull...)
}

// AddRawJSONFieldKey is AddRawJSONField with a precomputed key.
func (b *Builder) AddRawJSONFieldKey(k FieldKey, raw []byte) {
	b.precomputedKey(k)
	b.buf = append(b.buf, raw...)
}

// AddValueFieldKey is AddValueField with a precomputed key.
func (b *Builder) AddValueFieldKey(k FieldKey, v any, text func(v any) (string, bool)) {
	b.precomputedKey(k)
	b.appendValue(v, text)
}

// AddTimeRFC3339FieldKey is AddTimeRFC3339Field with a precomputed key.
func (b *Builder) AddTimeRFC3339FieldKey(k FieldKey, t time.Time) {
	b.precomputedKey(k)
	b.appendTimeRFC3339(t)
}

// AddTimeRFC3339OffsetFieldKey is AddTimeRFC3339OffsetField with a
// precomputed key.
func (b *Builder) AddTimeRFC3339OffsetFieldKey(k FieldKey, t time.Time) {
	b.precomputedKey(k)
	b.appendTimeRFC3339Offset(t)
}

// precomputedKey writes k, without its comma when no field precedes it.
func (b *Builder) precomputedKey(k FieldKey) {
	key := k.prefix[1:]
	if b.needSep {
		key = k.prefix
	}
	b.buf = append(b.buf, key...)
	b.needSep = true
}
