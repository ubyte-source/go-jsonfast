package jsonfast

import (
	"encoding/json"
	"testing"
	"time"
)

// nanoInstant is a UTC instant whose fraction uses every digit.
const nanoInstant = "2024-01-15T12:30:45.123456789Z"

// The first and the last instants the time writers reach.
const (
	firstInstant = "0000-01-01T00:00:00Z"
	lastInstant  = "9999-12-31T23:59:59.999999999Z"
)

// Zone offsets in seconds: a minute, an hour, a local mean time, offsets short
// of a minute, and the extremes on both sides of a day.
const (
	minute         = 60
	hour           = 60 * minute
	meanTimeOffset = 3*hour + 25*60 + 21
	halfMinuteWest = -30
	minuteAndHalf  = -90
	dayMinusMinute = 24*hour - 60
	day            = 24 * hour
	hundredHours   = 100 * hour
)

// lastYear is the last year the time writers write.
const lastYear = 9999

// fuzzSeconds bounds the Unix seconds the fuzz target writes, well past both
// ends of the range.
const fuzzSeconds = 1 << 40

// timeField writes t with AddTimeRFC3339Field and returns the quoted value.
func timeField(t time.Time) string {
	b := New(0)
	b.AddTimeRFC3339Field("t", t)
	return string(b.Bytes()[len(`"t":`):])
}

// offsetField writes t with AddTimeRFC3339OffsetField and returns the quoted
// value.
func offsetField(t time.Time) string {
	b := New(0)
	b.AddTimeRFC3339OffsetField("t", t)
	return string(b.Bytes()[len(`"t":`):])
}

// zoned returns the instant s in a zone offset seconds east of UTC.
func zoned(t *testing.T, s string, offset int) time.Time {
	t.Helper()
	return instant(t, s).In(time.FixedZone("", offset))
}

func TestBuilderAddTimeRFC3339FieldWritesUTC(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"2024-03-15T14:30:45.123456789Z", "2024-03-15T14:30:45.123456789Z"},
		{"2024-06-15T12:30:45Z", "2024-06-15T12:30:45Z"},
		{"2025-07-04T18:45:30.500Z", "2025-07-04T18:45:30.5Z"},
		{"2025-07-04T18:45:30.0000001Z", "2025-07-04T18:45:30.0000001Z"},
		{"2025-07-04T18:45:30.000000001Z", "2025-07-04T18:45:30.000000001Z"},
		{"2024-02-29T00:00:00+02:00", "2024-02-28T22:00:00Z"},
		{"2000-01-01T09:05:07Z", "2000-01-01T09:05:07Z"},
		{"1970-01-01T00:00:00Z", "1970-01-01T00:00:00Z"},
		{"1970-01-01T00:00:00.000000005Z", "1970-01-01T00:00:00.000000005Z"},
		{"1969-12-31T23:59:59Z", "1969-12-31T23:59:59Z"},
		{"1969-07-20T20:17:40Z", "1969-07-20T20:17:40Z"},
		{"0001-01-01T00:00:00Z", "0001-01-01T00:00:00Z"},
		{firstInstant, firstInstant},
		{"0000-01-01T00:00:00.000000005Z", "0000-01-01T00:00:00.000000005Z"},
		{"9999-12-31T23:59:59.000000005Z", "9999-12-31T23:59:59.000000005Z"},
		{lastInstant, lastInstant},
	} {
		if got := timeField(instant(t, tc.in)); got != quoted(tc.want) {
			t.Errorf("AddTimeRFC3339Field(%s) = %s, want %q", tc.in, got, tc.want)
		}
	}
	if got, want := timeField(time.Time{}), `"0001-01-01T00:00:00Z"`; got != want {
		t.Errorf("AddTimeRFC3339Field of the zero time = %s, want %s", got, want)
	}
}

func TestBuilderAddTimeRFC3339FieldClampsToTheRange(t *testing.T) {
	first, last := instant(t, firstInstant), instant(t, lastInstant)
	for _, tc := range []struct {
		in   time.Time
		want string
	}{
		{first.Add(-time.Nanosecond), firstInstant},
		{first.AddDate(-2, 0, 0), firstInstant},
		{zoned(t, "0000-01-01T00:30:00+01:00", 0), firstInstant},
		{last.Add(time.Nanosecond), lastInstant},
		{last.AddDate(2, 0, 0), lastInstant},
	} {
		if got := timeField(tc.in); got != quoted(tc.want) {
			t.Errorf("AddTimeRFC3339Field(%v) = %s, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuilderAddTimeRFC3339OffsetFieldKeepsWholeMinuteOffsets(t *testing.T) {
	for _, tc := range []struct {
		in   time.Time
		want string
	}{
		{instant(t, "2025-07-04T18:45:30Z"), "2025-07-04T18:45:30Z"},
		{instant(t, "2025-07-04T20:45:30+02:00"), "2025-07-04T20:45:30+02:00"},
		{instant(t, "2025-07-04T13:45:30-05:00"), "2025-07-04T13:45:30-05:00"},
		{instant(t, "2025-07-04T18:45:30.123456789+05:30"), "2025-07-04T18:45:30.123456789+05:30"},
		{zoned(t, "2024-01-15T08:34:39Z", meanTimeOffset), "2024-01-15T11:59:39+03:25"},
		{zoned(t, "2024-01-15T12:00:30Z", halfMinuteWest), "2024-01-15T12:00:30Z"},
		{zoned(t, "2024-01-15T12:01:30Z", minuteAndHalf), "2024-01-15T12:00:30-00:01"},
		{zoned(t, "2024-01-15T11:59:30Z", -minuteAndHalf), "2024-01-15T12:00:30+00:01"},
		{zoned(t, "2024-01-14T12:01:00Z", dayMinusMinute), "2024-01-15T12:00:00+23:59"},
		{zoned(t, "2024-01-16T11:59:00Z", -dayMinusMinute), "2024-01-15T12:00:00-23:59"},
		{instant(t, "1970-01-01T01:58:20+02:00"), "1970-01-01T01:58:20+02:00"},
		{instant(t, "0000-01-01T01:00:00+01:00"), "0000-01-01T01:00:00+01:00"},
		{zoned(t, "0000-01-01T01:00:00Z", -hour), "0000-01-01T00:00:00-01:00"},
		{instant(t, "1970-01-01T00:00:00-01:00"), "1970-01-01T00:00:00-01:00"},
		{instant(t, "9999-12-31T23:59:59+01:00"), "9999-12-31T23:59:59+01:00"},
	} {
		got := offsetField(tc.in)
		if got != quoted(tc.want) {
			t.Errorf("AddTimeRFC3339OffsetField(%v) = %s, want %q", tc.in, got, tc.want)
		}
		back, err := time.Parse(time.RFC3339Nano, got[1:len(got)-1])
		if err != nil || !back.Equal(clampOracle(t, tc.in)) {
			t.Errorf("%s parses to %v, %v, want the instant %v", got, back, err, tc.in)
		}
	}
}

func TestBuilderAddTimeRFC3339OffsetFieldFallsBackToUTC(t *testing.T) {
	for _, tc := range []struct {
		in   time.Time
		want string
	}{
		{zoned(t, "2024-01-14T12:00:00Z", day), "2024-01-14T12:00:00Z"},
		{zoned(t, "2024-01-16T12:00:00Z", -day), "2024-01-16T12:00:00Z"},
		{zoned(t, "2024-01-11T08:00:00Z", hundredHours), "2024-01-11T08:00:00Z"},
		{zoned(t, "0000-01-01T01:00:00Z", -2*hour), "0000-01-01T01:00:00Z"},
		{zoned(t, "0000-01-01T00:00:59Z", -minute), "0000-01-01T00:00:59Z"},
		{zoned(t, "9999-12-31T23:59:00Z", minute), "9999-12-31T23:59:00Z"},
		{zoned(t, "9999-12-31T23:30:00Z", 2*hour), "9999-12-31T23:30:00Z"},
		{instant(t, lastInstant).AddDate(2, 0, 0).In(time.FixedZone("", 2*hour)), lastInstant},
	} {
		if got := offsetField(tc.in); got != quoted(tc.want) {
			t.Errorf("AddTimeRFC3339OffsetField(%v) = %s, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuilderAddTimeRFC3339FieldAllocs(t *testing.T) {
	b := New(0)
	utc := instant(t, nanoInstant)
	east := instant(t, "2024-01-15T13:30:45.123456789+01:00")
	before := instant(t, firstInstant).AddDate(-1, 0, 0)
	assertAllocs(t, 0, func() {
		b.Reset()
		b.AddTimeRFC3339Field("a", utc)
		b.AddTimeRFC3339Field("b", east)
		b.AddTimeRFC3339OffsetField("c", east)
		b.AddTimeRFC3339OffsetField("d", before)
		b.BeginArrayField("e")
		b.AddTimeRFC3339Element(east)
		b.AddTimeRFC3339OffsetElement(east)
		b.EndArray()
	})
}

func TestBuilderAddTimeRFC3339ElementWritesTheFieldValue(t *testing.T) {
	east := zoned(t, nanoInstant, meanTimeOffset)
	b := New(0)
	b.BeginArray()
	b.AddTimeRFC3339Element(east)
	b.AddTimeRFC3339OffsetElement(east)
	b.AddTimeRFC3339Element(instant(t, lastInstant).AddDate(1, 0, 0))
	b.EndArray()
	want := "[" + quoted(nanoInstant) + "," + offsetField(east) + "," + quoted(lastInstant) + "]"
	if got := string(b.Bytes()); got != want || !json.Valid(b.Bytes()) {
		t.Fatalf("the time Element writers wrote %s, want %s", got, want)
	}
}

// clampOracle moves t into the range the writers cover.
func clampOracle(t *testing.T, in time.Time) time.Time {
	t.Helper()
	first, last := instant(t, firstInstant), instant(t, lastInstant)
	switch {
	case in.Before(first):
		return first.In(in.Location())
	case in.After(last):
		return last.In(in.Location())
	}
	return in
}

// offsetOracle returns what the offset writer writes for clamped, an instant
// in range written in UTC as utc, in a zone offset seconds east of UTC.
func offsetOracle(clamped time.Time, utc string, offset int) string {
	whole := offset / minute * minute
	if whole <= -day || whole >= day {
		return utc
	}
	if local := clamped.In(time.FixedZone("", whole)); local.Year() >= 0 && local.Year() <= lastYear {
		return local.Format(time.RFC3339Nano)
	}
	return utc
}

func FuzzBuilderAppendTimeRFC3339(f *testing.F) {
	for _, seed := range []string{firstInstant, lastInstant, "1970-01-01T00:00:00Z", nanoInstant} {
		at, err := time.Parse(time.RFC3339Nano, seed)
		if err != nil {
			f.Fatalf("time.Parse(%q) = %v, want nil", seed, err)
		}
		for _, offset := range []int{0, meanTimeOffset, minuteAndHalf, day, -day} {
			f.Add(at.Unix()-1, int64(at.Nanosecond()), offset)
			f.Add(at.Unix(), int64(at.Nanosecond()), offset)
		}
	}
	f.Fuzz(func(t *testing.T, sec, nsec int64, offset int) {
		if sec < -fuzzSeconds || sec > fuzzSeconds {
			return
		}
		in := time.Unix(sec, nsec).In(time.FixedZone("", offset))
		clamped := clampOracle(t, in)
		utc := clamped.UTC().Format(time.RFC3339Nano)
		if got := timeField(in); got != quoted(utc) {
			t.Fatalf("appendTimeRFC3339(%v) = %s, want %q", in, got, utc)
		}
		want := offsetOracle(clamped, utc, offset)
		if got := offsetField(in); got != quoted(want) {
			t.Fatalf("appendTimeRFC3339Offset(%v) = %s, want %q", in, got, want)
		}
		b := New(0)
		b.BeginArray()
		b.AddTimeRFC3339Element(in)
		b.AddTimeRFC3339OffsetElement(in)
		b.EndArray()
		if got := string(b.Bytes()); got != "["+quoted(utc)+","+quoted(want)+"]" {
			t.Fatalf("the time Element writers wrote %s for %v, want [%q,%q]", got, in, utc, want)
		}
		strictlyRead(t, b.Bytes())
	})
}

// stampName is the field name the time benchmarks write.
const stampName = "timestamp"

func BenchmarkBuilderAddTimeRFC3339Field(b *testing.B) {
	ts := instant(b, nanoInstant)
	benchWrite(b, quoted(stampName)+":"+quoted(nanoInstant), func(builder *Builder) {
		builder.AddTimeRFC3339Field(stampName, ts)
	})
}

func BenchmarkBuilderAddTimeRFC3339Element(b *testing.B) {
	ts := instant(b, nanoInstant)
	benchWrite(b, "["+quoted(nanoInstant)+","+quoted(nanoInstant)+"]", func(builder *Builder) {
		builder.BeginArray()
		builder.AddTimeRFC3339Element(ts)
		builder.AddTimeRFC3339OffsetElement(ts)
		builder.EndArray()
	})
}

func BenchmarkBuilderAddTimeRFC3339FieldTimeAppendFormat(b *testing.B) {
	name := []byte(stampName)
	ts := instant(b, nanoInstant)
	benchWrite(b, quoted(stampName)+":"+quoted(nanoInstant), func(builder *Builder) {
		builder.AddRawBytesField(name, nil)
		builder.buf = append(ts.UTC().AppendFormat(append(builder.buf, '"'), time.RFC3339Nano), '"')
	})
}

func BenchmarkBuilderAddTimeRFC3339OffsetField(b *testing.B) {
	const east = "2024-01-15T13:30:45.123456789+01:00"
	ts := instant(b, east)
	benchWrite(b, quoted(stampName)+":"+quoted(east), func(builder *Builder) {
		builder.AddTimeRFC3339OffsetField(stampName, ts)
	})
}
