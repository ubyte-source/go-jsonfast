GO            ?= go
GOLANGCI_LINT ?= GOTOOLCHAIN=go1.27.1 $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GOVULNCHECK   ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.8.0
DEADCODE      ?= $(GO) run golang.org/x/tools/cmd/deadcode@v0.50.0
FUZZTIME      ?= 30s
FUZZ_TARGETS  ?=
FUZZ_TAGS     ?=
NILAWAY       ?= $(GO) run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260918162853-acb8859b9031
TESTTIME_MAX  ?= 10
RACECOUNT     ?= 10
FINAL_FUZZTIME ?= 60s
# FUZZ_LIST prints pkg:Target for every fuzz target go test lists in the module.
FUZZ_LIST      = mod=$$($(GO) list -m) && out=$$($(GO) test -list='^Fuzz' ./...) && \
	printf '%s\n' "$$out" | awk -v mod="$$mod" '/^Fuzz/ {name[n++] = $$1} /^ok/ { \
		pkg = substr($$2, length(mod) + 2); if (pkg == "") pkg = "."; \
		for (i = 0; i < n; i++) print pkg ":" name[i]; n = 0 }'
# SLOW_TESTS selects the passing tests of testtime.json slower than TESTTIME_MAX.
SLOW_TESTS     = select(.Action == "pass" and .Test != null and .Elapsed > $(TESTTIME_MAX)) | \
	"\(.Package) \(.Test) \(.Elapsed)s"

.DEFAULT_GOAL := help

.PHONY: help ci modcheck vet lint fmt deadcode test race cover bench bench-smoke vuln fuzz \
	fuzz-list testtime final fmtcheck nilaway race-repeat fuzz-final treecheck vocab clean

help: ## List the targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

ci: modcheck vet lint vocab vuln deadcode test race bench-smoke cover ## Run every gate of the CI workflows

modcheck: ## Download and verify the modules; fail when go.mod or go.sum is not tidy
	$(GO) mod download
	$(GO) mod verify
	$(GO) mod tidy -diff

vet: ## Run go vet on the default and the purego build
	$(GO) vet ./...
	$(GO) vet -tags purego ./...

lint: ## Run golangci-lint with .golangci.yml on the default and the purego build
	$(GOLANGCI_LINT) run ./...
	$(GOLANGCI_LINT) run --build-tags purego ./...

fmt: ## Format the code with the formatters of .golangci.yml
	$(GOLANGCI_LINT) fmt ./...


deadcode: ## Fail on any function deadcode -test finds unreachable in either build
	@out=$$($(DEADCODE) -test ./... && $(DEADCODE) -test -tags purego ./...) && [ -z "$$out" ] || { echo "$$out"; exit 1; }

test: ## Run the tests of the default, the purego and the 32-bit build, allocation counts included
	$(GO) test -count=1 -shuffle=on ./...
	$(GO) test -count=1 -shuffle=on -tags purego ./...
	GOARCH=386 $(GO) test -count=1 -shuffle=on ./...

race: ## Run the tests of both builds with the race detector and write their coverage profiles
	CGO_ENABLED=1 $(GO) test -race -count=1 -shuffle=on -coverprofile=coverage.out -covermode=atomic ./...
	CGO_ENABLED=1 $(GO) test -race -count=1 -shuffle=on -tags purego \
		-coverprofile=coverage-purego.out -covermode=atomic ./...

cover: race ## Write coverage.html from the race run; fail when either build covers less than 100%
	$(GO) tool cover -html=coverage.out -o coverage.html
	@for f in coverage.out coverage-purego.out; do \
		$(GO) tool cover -func=$$f | awk -v f=$$f '$$1 == "total:" {print f, $$3; full = $$3 == "100.0%"} \
			END {exit !full}' || exit 1; \
	done

bench: ## Run every benchmark of the default and the purego build
	$(GO) test -run='^$$' -bench=. -benchmem ./...
	$(GO) test -run='^$$' -bench=. -benchmem -tags purego ./...

bench-smoke: ## Run every benchmark of both builds 100 times
	$(GO) test -run='^$$' -bench=. -benchtime=100x ./...
	$(GO) test -run='^$$' -bench=. -benchtime=100x -tags purego ./...

vuln: ## Scan the module and the toolchain with govulncheck
	$(GOVULNCHECK) ./...




vocab: ## Fail on claim names or token words in the Go and Markdown files outside tools/
	@out=$$(grep -rnE --include='*.go' --include='*.md' --exclude-dir=tools --exclude-dir=.git \
		'\\?"(sub|aud|iss|exp|nbf|iat|azp|scp|scope|client_id|nonce|kid|alg|typ)\\?"' .; \
		grep -rniwE --include='*.go' --include='*.md' --exclude-dir=tools --exclude-dir=.git \
		'jwt|jwks?|oauth|bearer|idp|openid' .); [ -z "$$out" ] || { echo "$$out"; exit 1; }

testtime: ## Fail on any test of either build slower than TESTTIME_MAX seconds in a serial race run (outside ci)
	{ CGO_ENABLED=1 $(GO) test -race -count=1 -p 1 -parallel 1 -json ./... && \
		CGO_ENABLED=1 $(GO) test -race -count=1 -p 1 -parallel 1 -json -tags purego ./...; } > testtime.json
	@slow=$$(jq -r '$(SLOW_TESTS)' testtime.json) && [ -z "$$slow" ] || { echo "$$slow"; exit 1; }

final: ## Run the delivery gates one after another on an idle machine
	@for gate in fmtcheck ci nilaway race-repeat fuzz-final testtime treecheck; do \
		$(MAKE) $$gate || exit 1; \
	done

fmtcheck: ## Fail when gofmt -s would change a file
	@out=$$("$$($(GO) env GOROOT)/bin/gofmt" -s -l .) && [ -z "$$out" ] || { echo "$$out"; exit 1; }

nilaway: ## Fail on any nil flow nilaway finds in the module, in either build
	$(NILAWAY) -include-pkgs=$$($(GO) list -m) ./...
	$(NILAWAY) -tags purego -include-pkgs=$$($(GO) list -m) ./...

race-repeat: ## Run the tests of both builds RACECOUNT times with the race detector
	CGO_ENABLED=1 $(GO) test -race -count=$(RACECOUNT) -shuffle=on ./...
	CGO_ENABLED=1 $(GO) test -race -count=$(RACECOUNT) -shuffle=on -tags purego ./...

fuzz-final: ## Run every fuzz target of both builds for FINAL_FUZZTIME
	$(MAKE) fuzz FUZZTIME=$(FINAL_FUZZTIME)
	$(MAKE) fuzz FUZZTIME=$(FINAL_FUZZTIME) FUZZ_TAGS=purego

treecheck: clean ## Fail on an artefact, an untracked non-source file, an empty directory or a non-LF file
	@out=$$(git status --porcelain=v1 --ignored --untracked-files=all | sed -n 's/^\(??\|!!\) //p' | \
		awk -v cmds="$$(ls cmd 2>/dev/null)" 'BEGIN { n = split(cmds, c, " "); for (i = 1; i <= n; i++) cmd[c[i]] = 1 }; \
		{ n = split($$0, p, "/"); b = p[n] }; \
		b ~ /\.(out|prof|pprof|test|exe|tmp|bak|orig|rej)$$|~$$|^coverage.*\.html$$/ || (b in cmd) { print; next }; \
		$$0 == ".env" || $$0 ~ /^\.(idea|vscode)\// || $$0 ~ /(^|\/)testdata\// { next }; \
		b ~ /\.(go|md|yml|yaml|json)$$/ { next }; \
		b ~ /^(go\.mod|go\.sum|LICENSE|Makefile|Dockerfile|\.gitignore|\.gitattributes|\.dockerignore)$$/ { next }; \
		b ~ /^(\.env\.example|default\.pgo)$$/ { next }; { print }') && [ -z "$$out" ] || \
		{ echo "treecheck: artefacts or untracked files of no source kind:"; echo "$$out"; exit 1; }
	@out=$$(find . -path ./.git -prune -o -type d -empty -print) && [ -z "$$out" ] || \
		{ echo "treecheck: empty directories:"; echo "$$out"; exit 1; }
	@out=$$(git ls-files --eol --cached --others --exclude-standard | awk '$$2 != "w/" && \
		($$1 !~ /^i\/(lf|-text|none)?$$/ || $$2 !~ /^w\/(lf|-text|none)$$/)') && [ -z "$$out" ] || \
		{ echo "treecheck: files not stored with LF (git add --renormalize . fixes the index):"; echo "$$out"; exit 1; }
	@grep -qx '\* text=auto eol=lf' .gitattributes || { echo "treecheck: .gitattributes lacks * text=auto eol=lf"; exit 1; }

fuzz: ## Run every fuzz target, or the pkg:Target list FUZZ_TARGETS, for FUZZTIME each
	@targets='$(FUZZ_TARGETS)'; [ -n "$$targets" ] || targets=$$($(FUZZ_LIST)) || exit 1; \
	for spec in $$targets; do \
		pkg=$${spec%%:*}; name=$${spec#*:}; \
		$(GO) test -list='^Fuzz' ./$$pkg | grep -qx "$$name" || \
			{ echo "fuzz: ./$$pkg has no fuzz target $$name" >&2; exit 1; }; \
		$(GO) test $(if $(FUZZ_TAGS),-tags $(FUZZ_TAGS)) -run='^$$' -fuzz="^$$name\$$" -fuzztime=$(FUZZTIME) ./$$pkg || exit 1; \
	done

fuzz-list: ## Print every fuzz target as the JSON array the fuzz workflow reads; fail when there is none
	@targets=$$($(FUZZ_LIST)) || exit 1; [ -n "$$targets" ] || { echo "fuzz-list: no fuzz target" >&2; exit 1; }; \
	sep=; printf '['; for spec in $$targets; do \
		printf '%s{"pkg":"%s","target":"%s"}' "$$sep" "$${spec%%:*}" "$${spec#*:}"; sep=,; \
	done; printf ']\n'

clean: ## Remove the test binaries, profiles, traces, coverage reports and testtime.json
	rm -f *.test *.prof *.pprof *.trace coverage.out coverage-purego.out coverage.html testtime.json
