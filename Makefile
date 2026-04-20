.PHONY: all test bench fuzz lint clean vet cover ci pgo alloc parallel

all: vet lint test bench

# Full CI pipeline: vet + lint + unit tests (race) + zero-alloc gate + coverage + benchmarks.
ci: vet lint test alloc cover bench

# Run all tests with the race detector.
test:
	go test -v -race -count=1 ./...

# Run only the zero-allocation assertions. Fails if any hot path regresses.
alloc:
	go test -run='^TestZeroAlloc_' -count=1 ./...

# Run benchmarks.
bench:
	go test -bench=. -benchmem -count=6 -run='^$$' ./...

# Run parallel benchmarks across a range of CPU counts.
parallel:
	go test -bench='^BenchmarkParallel_' -benchmem -count=3 -run='^$$' -cpu=1,4,8,16,32 ./...

# Run fuzzing for 30 seconds per target. Override FUZZTIME on the command
# line to extend runs: `make fuzz FUZZTIME=10m`.
FUZZTIME ?= 30s
fuzz:
	go test -fuzz=FuzzEscapeString    -fuzztime=$(FUZZTIME) ./...
	go test -fuzz=FuzzSkipStringAt    -fuzztime=$(FUZZTIME) ./...
	go test -fuzz=FuzzSkipBracedAt    -fuzztime=$(FUZZTIME) ./...
	go test -fuzz=FuzzIterateFields   -fuzztime=$(FUZZTIME) ./...
	go test -fuzz=FuzzFindField       -fuzztime=$(FUZZTIME) ./...
	go test -fuzz=FuzzIsStructuralJSON -fuzztime=$(FUZZTIME) ./...
	go test -fuzz=FuzzIterateArray    -fuzztime=$(FUZZTIME) ./...
	go test -fuzz=FuzzFlattenObject   -fuzztime=$(FUZZTIME) ./...

# Run go vet.
vet:
	go vet ./...

# Run staticcheck and golangci-lint if available.
lint:
	@if command -v staticcheck    >/dev/null 2>&1; then staticcheck -checks=all ./...; else echo "staticcheck not installed, skipping"; fi
	@if command -v golangci-lint  >/dev/null 2>&1; then golangci-lint run ./...;       else echo "golangci-lint not installed, skipping"; fi

# Regenerate the PGO profile from all benchmarks and replace default.pgo.
# Run before building for production so the compiler has accurate hot-path data.
pgo:
	go test -run='^$$' -bench=. -benchmem -count=5 -cpuprofile=cpu.prof .
	go tool pprof -proto cpu.prof > default.pgo
	@rm -f cpu.prof
	@echo "default.pgo updated — rebuild with: go build -pgo=auto ./..."

# Clean test cache and generated files.
clean:
	go clean -testcache
	@rm -f coverage.out coverage.html *.prof

# Show test coverage.
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out | tail -1
	@echo "Coverage report: coverage.html"
