package jsonfast

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Sizes the pool documents: the capacity of a new pooled Builder, and the
// largest buffer capacity the pool keeps.
const (
	pooledBuilder = 2 << 10
	keptBuilder   = 256 << 10
)

// drainBuilders empties the Builder pool.
func drainBuilders() {
	for {
		if builderPool.Get() == nil {
			return
		}
	}
}

func TestAcquireReturnsAnEmptyBuilder(t *testing.T) {
	drainBuilders()
	fresh := Acquire()
	if fresh.Len() != 0 || cap(fresh.buf) != pooledBuilder || fresh.needSep {
		t.Fatalf("Acquire on an empty pool = %d bytes, capacity %d, want 0, %d",
			fresh.Len(), cap(fresh.buf), pooledBuilder)
	}
	for range repeats {
		used := Acquire()
		used.BeginObject()
		used.AddStringField("k", "v")
		Release(used)
		if b := Acquire(); b.Len() != 0 || b.needSep {
			t.Fatalf("Acquire returned a Builder holding %q, want an empty one", b.Bytes())
		}
	}
}

func TestReleaseKeepsBuildersUpToTheLimit(t *testing.T) {
	Release(nil)
	oversized := &Builder{buf: make([]byte, 0, keptBuilder+1)}
	pooled := false
	for range repeats {
		drainBuilders()
		atLimit := &Builder{buf: make([]byte, 0, keptBuilder)}
		Release(oversized)
		Release(atLimit)
		switch b := Acquire(); b {
		case oversized:
			t.Fatalf("Acquire = the released Builder of capacity %d, want none past %d", cap(b.buf), keptBuilder)
		case atLimit:
			pooled = true
		}
	}
	if !pooled {
		t.Fatalf("Acquire never gave back a Builder of capacity %d in %d rounds, want it kept", keptBuilder, repeats)
	}
}

func TestAcquireAllocs(t *testing.T) {
	assertAllocs(t, 0, func() {
		b := Acquire()
		b.BeginObject()
		b.AddStringField("msg", "test")
		b.EndObject()
		Release(b)
	})
}

func TestBuildReturnsAnExactCopy(t *testing.T) {
	out := Build(func(b *Builder) {
		b.BeginObject()
		b.AddStringField("k", "v")
		b.EndObject()
	})
	if string(out) != `{"k":"v"}` || !capped(out) {
		t.Fatalf("Build = %q (cap %d), want an exact copy", out, cap(out))
	}
	again := Build(func(b *Builder) { b.AppendRawString("overwrite") })
	if string(out) != `{"k":"v"}` || string(again) != "overwrite" {
		t.Fatalf("a later Build changed an earlier result to %q, want %q", out, `{"k":"v"}`)
	}
	if empty := Build(func(*Builder) {}); empty == nil || len(empty) != 0 {
		t.Fatalf("Build of nothing = %#v, want a non-nil empty slice", empty)
	}
	var err error
	_ = Build(func(*Builder) { err = errStop })
	if !errors.Is(err, errStop) {
		t.Fatalf("the closure error after Build = %v, want errStop", err)
	}
}

func TestBuildReleasesItsBuilder(t *testing.T) {
	released := false
	for range repeats {
		drainBuilders()
		var used *Builder
		_ = Build(func(b *Builder) { used = b })
		if b, ok := builderPool.Get().(*Builder); ok && b == used {
			released = true
		}
	}
	if !released {
		t.Fatalf("the pool never held the Builder of Build in %d rounds, want it released", repeats)
	}
}

func TestBuildConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for g := range workers {
		wg.Go(func() {
			for i := range roundTrips {
				record := strconv.Itoa(g) + ":" + strconv.Itoa(i)
				out := Build(func(b *Builder) { b.AddStringElement(record) })
				if string(out) != `"`+record+`"` {
					t.Errorf("goroutine %d built %q, want %q", g, out, record)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestBuildAllocs(t *testing.T) {
	name := "captured"
	assertAllocs(t, 1, func() {
		_ = Build(func(b *Builder) {
			b.BeginObject()
			b.AddStringField("name", name)
			b.EndObject()
		})
	})
}

func FuzzBuild(f *testing.F) {
	f.Add([]byte(`{"a":1}`), 2)
	f.Add([]byte{}, 0)
	f.Fuzz(func(t *testing.T, part []byte, repeat int) {
		repeat = min(max(repeat, 0), workers)
		out := Build(func(b *Builder) {
			for range repeat {
				b.AppendRaw(part)
			}
		})
		want := bytes.Repeat(part, repeat)
		if !bytes.Equal(out, want) || out == nil || !capped(out) {
			t.Fatalf("Build of %d copies of %q = %q (cap %d), want %q capped", repeat, part, out, cap(out), want)
		}
		pooled := Acquire()
		defer Release(pooled)
		for range repeat {
			pooled.AppendRaw(part)
		}
		if !bytes.Equal(pooled.Bytes(), want) {
			t.Fatalf("a pooled Builder holds %q after %d copies of %q, want %q", pooled.Bytes(), repeat, part, want)
		}
	})
}

// writePooled writes the record of the pool benchmarks into a pooled Builder,
// which the caller releases.
func writePooled() *Builder {
	builder := Acquire()
	builder.BeginObject()
	builder.AddStringField("message", "User authentication failed")
	builder.AddIntField("severity", 1)
	builder.EndObject()
	return builder
}

// checkPooled fails b unless writePooled writes its record.
func checkPooled(b *testing.B) {
	b.Helper()
	builder := writePooled()
	defer Release(builder)
	if got, want := string(builder.Bytes()), `{"message":"User authentication failed","severity":1}`; got != want {
		b.Fatalf("a pooled Builder wrote %s, want %s", got, want)
	}
}

func BenchmarkAcquire(b *testing.B) {
	checkPooled(b)
	b.ReportAllocs()
	for b.Loop() {
		Release(writePooled())
	}
}

func BenchmarkAcquireParallel(b *testing.B) {
	checkPooled(b)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Release(writePooled())
		}
	})
}

func BenchmarkBuild(b *testing.B) {
	write := func(builder *Builder) {
		builder.BeginArray()
		builder.AddStringElement("alpha")
		builder.AddInt64Element(1)
		builder.EndArray()
	}
	if got, want := string(Build(write)), `["alpha",1]`; got != want {
		b.Fatalf("Build wrote %s, want %s", got, want)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = Build(write)
	}
}

func ExampleAcquire() {
	b := Acquire()
	defer Release(b)

	b.BeginObject()
	b.AddStringField("msg", "hello")
	b.AddIntField("sev", 2)
	b.AddTimeRFC3339Field("ts", time.Unix(0, 0))
	b.BeginArrayField("tags")
	b.AddStringElement("auth")
	b.AddStringElement("ssh")
	b.EndArray()
	b.EndObject()

	fmt.Println(string(b.Bytes()))
	// Output: {"msg":"hello","sev":2,"ts":"1970-01-01T00:00:00Z","tags":["auth","ssh"]}
}

func ExampleBuild() {
	out := Build(func(b *Builder) {
		b.BeginObject()
		b.BeginArrayField("ids")
		b.AddInt64Element(1)
		b.AddInt64Element(2)
		b.EndArray()
		b.AddBoolField("ok", true)
		b.EndObject()
	})
	fmt.Println(string(out))
	// Output: {"ids":[1,2],"ok":true}
}
