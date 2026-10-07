package jsonfast

import (
	"bytes"
	"time"
)

// AddTimeRFC3339Field writes "name":"YYYY-MM-DDThh:mm:ss[.fffffffff]Z", t in UTC
// with the trailing zeros of the fraction dropped; an instant before year 0 or
// after year 9999 is written as the nearest end of that range.
func (b *Builder) AddTimeRFC3339Field(name string, t time.Time) {
	b.fieldKey(name)
	b.appendTimeRFC3339(t)
}

// AddTimeRFC3339OffsetField is AddTimeRFC3339Field keeping the zone offset of
// t, truncated to whole minutes, as Z or ±hh:mm. When that offset spans a day
// or moves the wall clock out of the range, the instant is written in UTC.
func (b *Builder) AddTimeRFC3339OffsetField(name string, t time.Time) {
	b.fieldKey(name)
	b.appendTimeRFC3339Offset(t)
}

// AddTimeRFC3339Element writes "YYYY-MM-DDThh:mm:ss[.fffffffff]Z" as the next element,
// t in UTC with the trailing zeros of the fraction dropped; an instant before year 0
// or after year 9999 is written as the nearest end of that range.
func (b *Builder) AddTimeRFC3339Element(t time.Time) {
	b.sep()
	b.appendTimeRFC3339(t)
}

// AddTimeRFC3339OffsetElement is AddTimeRFC3339Element keeping the zone offset of t,
// truncated to whole minutes, as Z or ±hh:mm. When that offset spans a day or moves
// the wall clock out of the range, the instant is written in UTC.
func (b *Builder) AddTimeRFC3339OffsetElement(t time.Time) {
	b.sep()
	b.appendTimeRFC3339Offset(t)
}

// The first and the last second the time writers reach, 0000-01-01T00:00:00Z
// and 9999-12-31T23:59:59Z: every year four digits hold.
const (
	firstUnixSecond = -62167219200
	lastUnixSecond  = 253402300799
)

// Seconds per minute, and minutes per hour and day: a zone offset is written
// in whole minutes, below a day.
const (
	secondsPerMinute = 60
	minutesPerHour   = 60
	minutesPerDay    = 24 * minutesPerHour
)

// appendTimeRFC3339 writes the value of the UTC time writers.
func (b *Builder) appendTimeRFC3339(t time.Time) {
	b.appendClock(clampTime(t).UTC(), 0)
}

// appendTimeRFC3339Offset writes the value of the offset time writers.
func (b *Builder) appendTimeRFC3339Offset(t time.Time) {
	t = clampTime(t)
	_, offset := t.Zone()
	if minutes := offset / secondsPerMinute; minutes > -minutesPerDay && minutes < minutesPerDay {
		if wall := t.UTC().Add(time.Duration(minutes) * time.Minute); inTimeRange(wall) {
			b.appendClock(wall, minutes)
			return
		}
	}
	b.appendClock(t.UTC(), 0)
}

// clampTime moves an instant before year 0 or after year 9999 to the nearest
// end of that range, in the location of t.
func clampTime(t time.Time) time.Time {
	switch sec := t.Unix(); {
	case sec < firstUnixSecond:
		return time.Unix(firstUnixSecond, 0).In(t.Location())
	case sec > lastUnixSecond:
		return time.Unix(lastUnixSecond, int64(time.Second-1)).In(t.Location())
	}
	return t
}

// inTimeRange reports whether t lies between the start of year 0 and the end
// of year 9999.
func inTimeRange(t time.Time) bool {
	sec := t.Unix()
	return sec >= firstUnixSecond && sec <= lastUnixSecond
}

// appendClock writes the quoted wall clock of t, which is in UTC, with its
// fraction and the zone offset minutes east of UTC.
func (b *Builder) appendClock(t time.Time, minutes int) {
	year, month, day := t.Date()
	hour, minute, second := t.Clock()
	y1, y2 := pair(year / pairBase)
	y3, y4 := pair(year % pairBase)
	mo1, mo2 := pair(int(month))
	d1, d2 := pair(day)
	h1, h2 := pair(hour)
	mi1, mi2 := pair(minute)
	s1, s2 := pair(second)
	b.buf = append(b.buf, '"', y1, y2, y3, y4, '-', mo1, mo2, '-', d1, d2,
		'T', h1, h2, ':', mi1, mi2, ':', s1, s2)
	b.appendFraction(t.Nanosecond())
	b.appendZone(minutes)
	b.buf = append(b.buf, '"')
}

// appendFraction writes .fffffffff for a non-zero ns, trailing zeros dropped:
// the digits of ns plus one second, whose leading 1 becomes the dot.
func (b *Builder) appendFraction(ns int) {
	if ns == 0 {
		return
	}
	dot := len(b.buf)
	b.appendInt64(int64(ns) + int64(time.Second))
	b.buf[dot] = '.'
	b.buf = bytes.TrimRight(b.buf, "0")
}

// appendZone writes Z for a zero offset and ±hh:mm for any other, which is
// below a day.
func (b *Builder) appendZone(minutes int) {
	var sign byte
	switch {
	case minutes > 0:
		sign = '+'
	case minutes < 0:
		sign, minutes = '-', -minutes
	default:
		b.buf = append(b.buf, 'Z')
		return
	}
	h1, h2 := pair(minutes / minutesPerHour)
	m1, m2 := pair(minutes % minutesPerHour)
	b.buf = append(b.buf, sign, h1, h2, ':', m1, m2)
}
