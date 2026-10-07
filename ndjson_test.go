package jsonfast

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// lengthPrefixed splits data into the parts it packs, each after a byte that
// gives its length.
func lengthPrefixed(data []byte) []string {
	var parts []string
	for len(data) > 0 {
		n := min(int(data[0]), len(data)-1)
		parts = append(parts, string(data[1:n+1]))
		data = data[n+1:]
	}
	return parts
}

// newline ends every record of a batch.
const newline = "\n"

// batchRecords is how many records a batch of the BatchWriter tests holds.
const batchRecords = 16

// batchLine is the record that a batch of the BatchWriter tests repeats.
const batchLine = `{"timestamp":"2024-01-15T12:30:45Z","message":"test syslog message","severity":4}`

// defaultBatch is the capacity NewBatchWriter documents for a capacity below 1.
const defaultBatch = 4 << 10

func TestBatchWriterZeroValueIsReady(t *testing.T) {
	var w BatchWriter
	w.AppendString(objectA1)
	w.Append([]byte(oneArray))
	if got := string(w.Bytes()); got != objectA1+newline+oneArray+newline || w.Len() != len(got) || w.Count() != 2 {
		t.Fatalf("zero BatchWriter holds %q, Len %d, with %d records, want %q, %d, 2", got, w.Len(), w.Count(),
			objectA1+newline+oneArray+newline, len(got))
	}
}

func TestNewBatchWriterGivesTheCapacityOrTheDefault(t *testing.T) {
	for _, tc := range []struct{ capacity, want int }{
		{defaultBatch / 2, defaultBatch / 2}, {1, 1}, {0, defaultBatch}, {-1, defaultBatch},
	} {
		if w := NewBatchWriter(tc.capacity); w.Len() != 0 || w.Count() != 0 || cap(w.buf) != tc.want {
			t.Errorf("NewBatchWriter(%d) holds %d bytes with capacity %d, want none with %d",
				tc.capacity, w.Len(), cap(w.buf), tc.want)
		}
	}
	for _, n := range tooLarge() {
		if !panics(func() { NewBatchWriter(n) }) {
			t.Errorf("NewBatchWriter(%d) returned, want a panic: a capacity past what a byte slice can hold panics", n)
		}
	}
}

func TestBatchWriterAppendsOneRecordPerLine(t *testing.T) {
	w := NewBatchWriter(1)
	w.Append([]byte(`{"line":1}`))
	w.AppendString(`{"line":2}`)
	want := "{\"line\":1}\n{\"line\":2}\n"
	if got := string(w.Bytes()); got != want || w.Len() != len(want) || w.Count() != 2 {
		t.Fatalf("batch = %q, Len %d, Count %d, want %q, %d, 2", got, w.Len(), w.Count(), want, len(want))
	}
}

func TestBatchWriterResetEmptiesAndKeepsTheBuffer(t *testing.T) {
	w := NewBatchWriter(0)
	w.AppendString("record")
	w.Reset()
	if w.Len() != 0 || w.Count() != 0 || cap(w.buf) != defaultBatch {
		t.Fatalf("Reset left %d bytes, %d records, capacity %d, want 0, 0, %d",
			w.Len(), w.Count(), cap(w.buf), defaultBatch)
	}
}

func TestBatchWriterGrowMakesRoomOnce(t *testing.T) {
	const room = 1 << 10
	w := NewBatchWriter(1)
	w.AppendString("abc")
	w.Grow(room)
	if cap(w.buf)-w.Len() < room || string(w.Bytes()) != "abc\n" {
		t.Fatalf("Grow(%d) left capacity %d for %q, want room for %d more", room, cap(w.buf), w.Bytes(), room)
	}
	before := cap(w.buf)
	w.Grow(room / 2)
	if cap(w.buf) != before {
		t.Fatalf("Grow with room reallocated: capacity %d, want %d", cap(w.buf), before)
	}
	for _, n := range append([]int{-1}, tooLarge()...) {
		got, want := panics(func() { w.Grow(n) }), panics(func() { _ = slices.Grow(w.buf, n) })
		if !got || got != want {
			t.Errorf("Grow(%d) panicked: %v; slices.Grow panicked: %v, want both", n, got, want)
		}
	}
}

func TestBatchWriterAppendAllocs(t *testing.T) {
	w := NewBatchWriter(0)
	line := []byte(batchLine)
	size := batchRecords * (len(line) + len(newline) + len(emptyObject) + len(newline))
	assertAllocs(t, 0, func() {
		w.Reset()
		w.Grow(size)
		for range batchRecords {
			w.Append(line)
			w.AppendString(emptyObject)
		}
		if w.Count() != 2*batchRecords || w.Len() != size || len(w.Bytes()) != size {
			t.Fatalf("Count = %d, Len = %d, Bytes %d long; want %d records in %d bytes", w.Count(), w.Len(),
				len(w.Bytes()), 2*batchRecords, size)
		}
	})
	record := []byte(strings.Repeat("r", defaultBatch/2))
	assertAllocs(t, 2, func() { NewBatchWriter(1).Append(record) })
	assertAllocs(t, 1, func() {
		var grown BatchWriter
		grown.Grow(len(record) + 1)
		grown.Append(record)
	})
}

// fillBatch appends records to w, through Append and AppendString in turn.
func fillBatch(w *BatchWriter, records []string) {
	for i, record := range records {
		if i%2 == 0 {
			w.Append([]byte(record))
		} else {
			w.AppendString(record)
		}
	}
}

func FuzzBatchWriterAppend(f *testing.F) {
	f.Add([]byte("\x07{\"a\":1}\x00\x03[1]"))
	f.Fuzz(func(t *testing.T, data []byte) {
		records := lengthPrefixed(data)
		var want strings.Builder
		for _, record := range records {
			want.WriteString(record + newline)
		}
		for _, w := range []*BatchWriter{NewBatchWriter(1), AcquireBatchWriter()} {
			for range 2 {
				fillBatch(w, records)
				if string(w.Bytes()) != want.String() || w.Count() != len(records) || w.Len() != want.Len() {
					t.Fatalf("batch = %q with %d records, want %q with %d", w.Bytes(), w.Count(), want.String(),
						len(records))
				}
				w.Reset()
				w.Grow(want.Len())
			}
			ReleaseBatchWriter(w)
		}
	})
}

// checkBatch fails tb unless w holds batchRecords copies of batchLine.
func checkBatch(tb testing.TB, w *BatchWriter) {
	tb.Helper()
	batch := strings.Repeat(batchLine+newline, batchRecords)
	if w.Count() != batchRecords || w.Len() != len(batch) || string(w.Bytes()) != batch {
		tb.Fatalf("the batch holds %d records in %d bytes, %q; want %d of %q", w.Count(), w.Len(), w.Bytes(),
			batchRecords, batchLine)
	}
}

// benchBatch fails b unless fill writes the batch checkBatch expects into a
// BatchWriter; then it times fill on one BatchWriter, reset before each run, and
// reports the bytes one run writes.
func benchBatch(b *testing.B, fill func(w *BatchWriter)) {
	b.Helper()
	w := NewBatchWriter(0)
	fill(w)
	checkBatch(b, w)
	b.ReportAllocs()
	b.SetBytes(int64(w.Len()))
	for b.Loop() {
		w.Reset()
		fill(w)
	}
}

func BenchmarkBatchWriterAppend(b *testing.B) {
	line := []byte(batchLine)
	benchBatch(b, func(w *BatchWriter) {
		w.Grow(batchRecords * (len(line) + 1))
		for range batchRecords {
			w.Append(line)
		}
	})
}

func BenchmarkBatchWriterAppendString(b *testing.B) {
	benchBatch(b, func(w *BatchWriter) {
		w.Grow(batchRecords * (len(batchLine) + 1))
		for range batchRecords {
			w.AppendString(batchLine)
		}
	})
}

func ExampleBatchWriter() {
	w := NewBatchWriter(0)
	w.Append([]byte(`{"line":1}`))
	w.AppendString(`{"line":2}`)
	fmt.Print(string(w.Bytes()))
	// Output:
	// {"line":1}
	// {"line":2}
}

// Sizes the BatchWriter pool documents: the capacity of a new pooled writer,
// and the largest buffer capacity the pool keeps.
const (
	pooledBatch = 8 << 10
	keptBatch   = 4 << 20
)

// drainBatchWriters empties the BatchWriter pool.
func drainBatchWriters() {
	for {
		if batchPool.Get() == nil {
			return
		}
	}
}

func TestAcquireBatchWriterReturnsAnEmptyWriter(t *testing.T) {
	drainBatchWriters()
	fresh := AcquireBatchWriter()
	if fresh.Len() != 0 || fresh.Count() != 0 || cap(fresh.buf) != pooledBatch {
		t.Fatalf("AcquireBatchWriter on an empty pool = %d bytes, capacity %d, want 0, %d", fresh.Len(), cap(fresh.buf),
			pooledBatch)
	}
	for range repeats {
		used := AcquireBatchWriter()
		used.AppendString("first")
		ReleaseBatchWriter(used)
		if w := AcquireBatchWriter(); w.Len() != 0 || w.Count() != 0 {
			t.Fatalf("AcquireBatchWriter returned a writer holding %q, want an empty one", w.Bytes())
		}
	}
}

func TestReleaseBatchWriterKeepsWritersUpToTheLimit(t *testing.T) {
	ReleaseBatchWriter(nil)
	oversized := &BatchWriter{buf: make([]byte, 0, keptBatch+1)}
	pooled := false
	for range repeats {
		drainBatchWriters()
		atLimit := &BatchWriter{buf: make([]byte, 0, keptBatch)}
		ReleaseBatchWriter(oversized)
		ReleaseBatchWriter(atLimit)
		switch w := AcquireBatchWriter(); w {
		case oversized:
			t.Fatalf("AcquireBatchWriter = the released writer of capacity %d, want none past %d",
				cap(w.buf), keptBatch)
		case atLimit:
			pooled = true
		}
	}
	if !pooled {
		t.Fatalf("AcquireBatchWriter never gave back a writer of capacity %d in %d rounds, want it kept",
			keptBatch, repeats)
	}
}

func TestAcquireBatchWriterConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for g := range workers {
		wg.Go(func() {
			for i := range roundTrips {
				record := strconv.Itoa(g) + ":" + strconv.Itoa(i)
				w := AcquireBatchWriter()
				w.AppendString(record)
				if string(w.Bytes()) != record+newline || w.Count() != 1 {
					t.Errorf("goroutine %d holds %q in %d records, want only %q", g, w.Bytes(), w.Count(), record)
					return
				}
				ReleaseBatchWriter(w)
			}
		})
	}
	wg.Wait()
}

func TestAcquireBatchWriterAllocs(t *testing.T) {
	line := []byte(`{"msg":"test"}`)
	assertAllocs(t, 0, func() {
		w := AcquireBatchWriter()
		w.Append(line)
		ReleaseBatchWriter(w)
	})
}

func BenchmarkAcquireBatchWriterParallel(b *testing.B) {
	line := []byte(batchLine)
	fill := func() *BatchWriter {
		w := AcquireBatchWriter()
		for range batchRecords {
			w.Append(line)
		}
		return w
	}
	w := fill()
	checkBatch(b, w)
	ReleaseBatchWriter(w)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ReleaseBatchWriter(fill())
		}
	})
}

func ExampleAcquireBatchWriter() {
	w := AcquireBatchWriter()
	defer ReleaseBatchWriter(w)
	b := Acquire()
	defer Release(b)

	for _, msg := range []string{"first", "second"} {
		b.Reset()
		b.BeginObject()
		b.AddStringField("msg", msg)
		b.EndObject()
		w.Append(b.Bytes())
	}
	fmt.Print(string(w.Bytes()))
	// Output:
	// {"msg":"first"}
	// {"msg":"second"}
}
