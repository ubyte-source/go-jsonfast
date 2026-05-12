package jsonfast

// Shared test fixture constants and helpers reused across the test suite.

const (
	testSDID         = "exampleSDID@32473"
	testSDPriority   = "examplePriority@32473"
	testIPv4Host     = "10.0.0.1"
	testIPv4Peer     = "10.0.0.2"
	testHostnameFW01 = "fw01"
	testJSONHelloLit = `"hello"`
	testJSONObjA1    = `{"a":1}`
	testJSONObjB2    = `{"b":2}`
	testKey          = "key"
)

// testSDFixture returns the canonical RFC 5424 example structured-data map
// with both the SDID and priority blocks.
func testSDFixture() map[string]map[string]string {
	return map[string]map[string]string{
		testSDID: {
			"iut":         "3",
			"eventSource": "Application",
			"eventID":     "1011",
		},
		testSDPriority: {
			"class": "high",
		},
	}
}

// testSDOnlyFixture returns testSDFixture without the priority block.
func testSDOnlyFixture() map[string]map[string]string {
	return map[string]map[string]string{
		testSDID: {
			"iut":         "3",
			"eventSource": "Application",
			"eventID":     "1011",
		},
	}
}
