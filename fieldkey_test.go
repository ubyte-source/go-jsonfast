package jsonfast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNewFieldKeyBuildsThePrefix(t *testing.T) {
	if got, want := NewFieldKey("level"), (FieldKey{prefix: `,"level":`}); got != want {
		t.Fatalf("NewFieldKey(level) = %q, want %q", got.prefix, want.prefix)
	}
	for c := byte(' '); c < utf8.RuneSelf; c++ {
		if c == '"' || c == '\\' {
			continue
		}
		name := "k" + string([]byte{c})
		if got, want := NewFieldKey(name).prefix, `,"`+name+`":`; got != want {
			t.Fatalf("NewFieldKey(%q) = %q, want %q", name, got, want)
		}
	}
	if got := NewFieldKey(""); got.prefix != `,"":` {
		t.Fatalf("NewFieldKey(\"\") = %q, want the empty name", got.prefix)
	}
}

func TestNewFieldKeyEscapesTheName(t *testing.T) {
	names := []string{`quo"te`, `back\slash`, "tab\there", "nul\x00", "us\x1f", "high\x80", cafe, "\u2028<>"}
	for _, name := range names {
		want := "," + stdJSON(t, string([]rune(name))) + ":"
		if got := NewFieldKey(name).prefix; encodingJSONForm([]byte(got)) != want {
			t.Errorf("NewFieldKey(%q) = %q, want %q", name, got, want)
		}
	}
}

// keyedValues are the values the FieldKey writers and their named twins write.
type keyedValues struct {
	str string
	ts  time.Time
	i64 int64
	u64 uint64
	f64 float64
}

// fieldKeyTwin is a FieldKey writer and its named twin, which write the same.
type fieldKeyTwin struct {
	name          string
	keyed, byName func(b *Builder)
}

// fieldKeyTwins returns each FieldKey writer of NewFieldKey(name) with its named
// twin, both writing the values of kv.
func fieldKeyTwins(name string, kv keyedValues) []fieldKeyTwin {
	key := NewFieldKey(name)
	values, rawArray, str := []string{kv.str, kv.str}, []byte(oneArray), []byte(kv.str)
	value := map[string]any{kv.str: []any{kv.i64, kv.f64, kv.str}}
	return []fieldKeyTwin{
		{"String",
			func(b *Builder) { b.AddStringFieldKey(key, kv.str) },
			func(b *Builder) { b.AddStringField(name, kv.str) }},
		{"StringBytes",
			func(b *Builder) { b.AddStringBytesFieldKey(key, str) },
			func(b *Builder) { b.AddStringBytesField(name, str) }},
		{"Value",
			func(b *Builder) { b.AddValueFieldKey(key, value, nil) },
			func(b *Builder) { b.AddValueField(name, value, nil) }},
		{"EmptyRawJSON",
			func(b *Builder) { b.AddRawJSONFieldKey(key, nil); b.BeginArray(); b.EndArray() },
			func(b *Builder) { b.AddRawJSONField(name, nil); b.BeginArray(); b.EndArray() }},
		{"StringArray",
			func(b *Builder) { b.AddStringArrayFieldKey(key, values) },
			func(b *Builder) { b.AddStringArrayField(name, values) }},
		{"RawJSON",
			func(b *Builder) { b.AddRawJSONFieldKey(key, rawArray) },
			func(b *Builder) { b.AddRawJSONField(name, rawArray) }},
		{"BoolTrue",
			func(b *Builder) { b.AddBoolFieldKey(key, true) },
			func(b *Builder) { b.AddBoolField(name, true) }},
		{"BoolFalse",
			func(b *Builder) { b.AddBoolFieldKey(key, false) },
			func(b *Builder) { b.AddBoolField(name, false) }},
		{"Null",
			func(b *Builder) { b.AddNullFieldKey(key) },
			func(b *Builder) { b.AddNullField(name) }},
		{"Int",
			func(b *Builder) { b.AddIntFieldKey(key, int(kv.i64)) },
			func(b *Builder) { b.AddIntField(name, int(kv.i64)) }},
		{"Int64",
			func(b *Builder) { b.AddInt64FieldKey(key, kv.i64) },
			func(b *Builder) { b.AddInt64Field(name, kv.i64) }},
		{"Uint64",
			func(b *Builder) { b.AddUint64FieldKey(key, kv.u64) },
			func(b *Builder) { b.AddUint64Field(name, kv.u64) }},
		{"Float64",
			func(b *Builder) { b.AddFloat64FieldKey(key, kv.f64) },
			func(b *Builder) { b.AddFloat64Field(name, kv.f64) }},
		{"Time",
			func(b *Builder) { b.AddTimeRFC3339FieldKey(key, kv.ts) },
			func(b *Builder) { b.AddTimeRFC3339Field(name, kv.ts) }},
		{"TimeOffset",
			func(b *Builder) { b.AddTimeRFC3339OffsetFieldKey(key, kv.ts) },
			func(b *Builder) { b.AddTimeRFC3339OffsetField(name, kv.ts) }},
		{"Object",
			func(b *Builder) { b.BeginObjectFieldKey(key); b.EndObject() },
			func(b *Builder) { b.BeginObjectField(name); b.EndObject() }},
		{"Array",
			func(b *Builder) { b.BeginArrayFieldKey(key); b.EndArray() },
			func(b *Builder) { b.BeginArrayField(name); b.EndArray() }},
	}
}

// assertTwins fails t unless each FieldKey writer of twins, run twice, writes
// the members of a JSON object, as its named twin writes them.
func assertTwins(t *testing.T, twins []fieldKeyTwin) {
	t.Helper()
	for _, tc := range twins {
		keyed, named := New(0), New(0)
		for range 2 {
			tc.keyed(keyed)
			tc.byName(named)
		}
		wrapped := "{" + string(keyed.Bytes()) + "}"
		if !bytes.Equal(keyed.Bytes(), named.Bytes()) || !json.Valid([]byte(wrapped)) {
			t.Errorf("%s: FieldKey wrote %s, want %s as the named field writes", tc.name, keyed.Bytes(), named.Bytes())
		}
		once := New(0)
		once.BeginObject()
		tc.keyed(once)
		once.EndObject()
		strictlyRead(t, once.Bytes())
	}
}

func TestFieldKeyMethodsMatchTheirNamedTwins(t *testing.T) {
	const name = "f"
	v := keyedValues{
		str: "v\n", ts: instant(t, "2025-07-04T18:45:30.0000005+02:00"),
		i64: math.MinInt64, u64: math.MaxUint64, f64: math.SmallestNonzeroFloat64,
	}
	assertTwins(t, fieldKeyTwins(name, v))
}

func FuzzNewFieldKey(f *testing.F) {
	// The seconds of the seeds are also whole-minute zone offsets, east and west.
	for i, name := range []string{"level", "", `q"t`, `b\s`, "tab\t", "caf\u00e9", "~ !\x7f"} {
		sec := int64(time.Hour/time.Second) * int64(1-2*(i%2))
		f.Add(name, "v\n\xff", int64(math.MinInt64), uint64(math.MaxUint64), math.SmallestNonzeroFloat64, sec)
	}
	f.Fuzz(func(t *testing.T, name, str string, i64 int64, u64 uint64, f64 float64, sec int64) {
		ts := time.Unix(sec, 0).In(time.FixedZone("", int(sec%int64(24*time.Hour/time.Second))))
		assertTwins(t, fieldKeyTwins(name, keyedValues{str: str, ts: ts, i64: i64, u64: u64, f64: f64}))
	})
}

func TestFieldKeyZeroValuePanicsInEveryPosition(t *testing.T) {
	var zero FieldKey
	for name, write := range map[string]func(b *Builder){
		"First": func(b *Builder) { b.AddStringFieldKey(zero, "v") },
		"AfterAField": func(b *Builder) {
			b.AddIntField("a", 1)
			b.AddStringFieldKey(zero, "v")
		},
		"AfterAnObject": func(b *Builder) {
			b.BeginObject()
			b.EndObject()
			b.BeginObjectFieldKey(zero)
		},
	} {
		if !panics(func() { write(New(0)) }) {
			t.Errorf("%s: the zero FieldKey wrote a field, want a panic", name)
		}
	}
}

func TestFieldKeyAllocs(t *testing.T) {
	b := New(0)
	k := NewFieldKey("k")
	ts := instant(t, "2024-01-15T12:30:45.123456789Z")
	values := []string{"a", "b"}
	raw := []byte(objectA1)
	value := map[string]any{plainText: []any{true, nil}}
	assertAllocs(t, 0, func() {
		b.Reset()
		b.BeginObject()
		b.AddStringFieldKey(k, "v")
		b.AddStringBytesFieldKey(k, raw)
		b.AddValueFieldKey(k, value, nil)
		b.AddStringArrayFieldKey(k, values)
		b.AddRawJSONFieldKey(k, raw)
		b.AddBoolFieldKey(k, true)
		b.AddNullFieldKey(k)
		b.AddIntFieldKey(k, math.MaxInt)
		b.AddInt64FieldKey(k, math.MinInt64)
		b.AddUint64FieldKey(k, math.MaxUint64)
		b.AddFloat64FieldKey(k, math.Pi)
		b.AddTimeRFC3339FieldKey(k, ts)
		b.AddTimeRFC3339OffsetFieldKey(k, ts)
		b.BeginObjectFieldKey(k)
		b.EndObject()
		b.BeginArrayFieldKey(k)
		b.EndArray()
		b.EndObject()
	})
	var key FieldKey
	assertAllocs(t, 1, func() { key = NewFieldKey("name") })
	assertAllocs(t, 2, func() { key = NewFieldKey("na\"me") })
	b.Reset()
	b.AddNullFieldKey(key)
	if got, want := string(b.Bytes()), `"na\"me":null`; got != want {
		t.Fatalf("the key of na\"me wrote %s, want %s", got, want)
	}
}

func BenchmarkFieldKeySyslogObject(b *testing.B) {
	message, hostname, severity := NewFieldKey("message"), NewFieldKey("hostname"), NewFieldKey("severity")
	facility, appName, procID := NewFieldKey("facility"), NewFieldKey("app_name"), NewFieldKey("proc_id")
	msgID, version, source := NewFieldKey("msg_id"), NewFieldKey("version"), NewFieldKey("source")
	benchWrite(b, syslogRecord(b), func(builder *Builder) {
		builder.BeginObject()
		builder.AddStringFieldKey(message, logMessage)
		builder.AddStringFieldKey(hostname, logHost)
		builder.AddIntFieldKey(severity, logSeverity)
		builder.AddIntFieldKey(facility, logFacility)
		builder.AddStringFieldKey(appName, logApp)
		builder.AddStringFieldKey(procID, logProc)
		builder.AddStringFieldKey(msgID, logMsgID)
		builder.AddIntFieldKey(version, logVersion)
		builder.AddStringFieldKey(source, logSource)
		builder.EndObject()
	})
}

func BenchmarkFieldKeyFields(b *testing.B) {
	strKey, strs, num, num64 := NewFieldKey("s"), NewFieldKey("a"), NewFieldKey("i"), NewFieldKey("i64")
	unsigned, float, flag, null := NewFieldKey("u"), NewFieldKey("f"), NewFieldKey("b"), NewFieldKey("n")
	rawKey, utc, offset := NewFieldKey("r"), NewFieldKey("t"), NewFieldKey("z")
	objectKey, list, bytesKey, valueKey := NewFieldKey("o"), NewFieldKey("l"), NewFieldKey("p"), NewFieldKey("v")
	ts := instant(b, "2024-01-15T13:30:45.123456789+01:00")
	values, raw, value := []string{"x", "y"}, []byte(objectA1), any([]any{"x", true})
	want := `{"s":"v","p":"{\"a\":1}","v":["x",true],"a":["x","y"],"i":-7,"i64":-9223372036854775808,` +
		`"u":18446744073709551615,"f":3.141592653589793,"b":true,"n":null,"r":{"a":1},` +
		`"t":"2024-01-15T12:30:45.123456789Z","z":"2024-01-15T13:30:45.123456789+01:00","o":{},"l":[]}`
	benchWrite(b, want, func(builder *Builder) {
		builder.BeginObject()
		builder.AddStringFieldKey(strKey, "v")
		builder.AddStringBytesFieldKey(bytesKey, raw)
		builder.AddValueFieldKey(valueKey, value, nil)
		builder.AddStringArrayFieldKey(strs, values)
		builder.AddIntFieldKey(num, -7)
		builder.AddInt64FieldKey(num64, math.MinInt64)
		builder.AddUint64FieldKey(unsigned, math.MaxUint64)
		builder.AddFloat64FieldKey(float, math.Pi)
		builder.AddBoolFieldKey(flag, true)
		builder.AddNullFieldKey(null)
		builder.AddRawJSONFieldKey(rawKey, raw)
		builder.AddTimeRFC3339FieldKey(utc, ts)
		builder.AddTimeRFC3339OffsetFieldKey(offset, ts)
		builder.BeginObjectFieldKey(objectKey)
		builder.EndObject()
		builder.BeginArrayFieldKey(list)
		builder.EndArray()
		builder.EndObject()
	})
}

func ExampleNewFieldKey() {
	keyLevel := NewFieldKey("level")
	b := New(0)
	b.BeginObject()
	b.AddStringField("msg", "up")
	b.AddIntFieldKey(keyLevel, 2)
	b.EndObject()
	fmt.Println(string(b.Bytes()))
	// Output: {"msg":"up","level":2}
}
