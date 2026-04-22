package jsonfast

import (
	"testing"
	"time"
)

// Parallel benchmarks (run with `make parallel`).

func BenchmarkParallel_Builder_AcquireRelease(b *testing.B) {
	WarmPool(64)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bb := Acquire()
			bb.BeginObject()
			bb.AddStringField("msg", "test")
			bb.EndObject()
			_ = bb.Bytes()
			Release(bb)
		}
	})
}

func BenchmarkParallel_Builder_FullSyslog(b *testing.B) {
	WarmPool(64)
	ts := time.Date(2024, 1, 15, 12, 30, 45, 123456789, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bb := Acquire()
			bb.BeginObject()
			bb.AddStringField("message", "User authentication failed")
			bb.AddTimeRFC3339Field("timestamp", ts)
			bb.AddStringField("hostname", "webserver-prod-01")
			bb.AddIntField("severity", 4)
			bb.AddStringField("app_name", "sshd")
			bb.AddStringField("source", "192.168.1.100")
			bb.EndObject()
			_ = bb.Bytes()
			Release(bb)
		}
	})
}

func BenchmarkParallel_BatchWriter_AcquireAppend(b *testing.B) {
	WarmBatchWriterPool(64)
	line := []byte(`{"timestamp":"2024-01-15T12:30:45Z","message":"test","severity":4}`)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bw := AcquireBatchWriter()
			for range 16 {
				bw.Append(line)
			}
			ReleaseBatchWriter(bw)
		}
	})
}

func BenchmarkParallel_FindField(b *testing.B) {
	data := []byte(`{"facility":23,"severity":3,"hostname":"FW01","app_name":"utm","source":"10.0.0.1"}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = FindField(data, "source")
		}
	})
}

func BenchmarkParallel_EscapeString_PureASCII(b *testing.B) {
	s := "This is a typical syslog message with hostname=myhost " +
		"severity=info facility=local0 and no special characters at all"
	b.ReportAllocs()
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = EscapeString(s)
		}
	})
}
