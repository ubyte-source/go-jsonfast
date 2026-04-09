package jsonfast_test

import (
	"fmt"
	"time"

	"github.com/ubyte-source/go-jsonfast"
)

func ExampleBuilder() {
	b := jsonfast.New(256)
	b.BeginObject()
	b.AddStringField("message", "Hello, World!")
	b.AddIntField("severity", 5)
	b.AddBoolField("active", true)
	b.EndObject()

	fmt.Println(string(b.Bytes()))
	// Output: {"message":"Hello, World!","severity":5,"active":true}
}

func ExampleBuilder_withTimestamp() {
	b := jsonfast.New(256)
	ts := time.Date(2024, 3, 15, 14, 30, 45, 0, time.UTC)

	b.BeginObject()
	b.AddStringField("msg", "test")
	b.AddTimeRFC3339Field("ts", ts)
	b.EndObject()

	fmt.Println(string(b.Bytes()))
	// Output: {"msg":"test","ts":"2024-03-15T14:30:45Z"}
}

func ExampleEscapeString() {
	escaped := jsonfast.EscapeString(`She said "hello" and left`)
	fmt.Println(escaped)
	// Output: She said \"hello\" and left
}

func ExampleBuilder_Grow() {
	b := jsonfast.New(16)
	b.Grow(1024)
	b.BeginObject()
	b.AddStringField("k", "v")
	b.EndObject()

	fmt.Println(string(b.Bytes()))
	// Output: {"k":"v"}
}

func ExampleAcquire() {
	b := jsonfast.Acquire()
	defer jsonfast.Release(b)

	b.BeginObject()
	b.AddStringField("source", "pool")
	b.EndObject()

	fmt.Println(string(b.Bytes()))
	// Output: {"source":"pool"}
}

func ExampleBatchWriter() {
	bw := jsonfast.NewBatchWriter(256)
	bw.Append([]byte(`{"line":1}`))
	bw.Append([]byte(`{"line":2}`))

	fmt.Print(string(bw.Bytes()))
	// Output:
	// {"line":1}
	// {"line":2}
}

func ExampleAcquireBatchWriter() {
	bw := jsonfast.AcquireBatchWriter()
	defer jsonfast.ReleaseBatchWriter(bw)

	bw.Append([]byte(`{"pooled":true}`))
	fmt.Print(string(bw.Bytes()))
	// Output:
	// {"pooled":true}
}

func ExampleFlattenMap() {
	m := map[string]map[string]string{
		"host": {"name": "fw01", "ip": "10.0.0.1"},
	}
	flat := jsonfast.FlattenMap(m, nil)
	fmt.Println(flat["host.name"])
	fmt.Println(flat["host.ip"])
	// Output:
	// fw01
	// 10.0.0.1
}

func ExampleBuilder_AddFloat64Field() {
	b := jsonfast.New(64)
	b.BeginObject()
	b.AddFloat64Field("pi", 3.14159)
	b.EndObject()

	fmt.Println(string(b.Bytes()))
	// Output: {"pi":3.14159}
}

func ExampleIsLikelyJSON() {
	fmt.Println(jsonfast.IsLikelyJSON(`{"key":"value"}`))
	fmt.Println(jsonfast.IsLikelyJSON(`not json`))
	// Output:
	// true
	// false
}
