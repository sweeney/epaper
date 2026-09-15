# epaper — see PLAN.md §11.5 for the development loop.
#
# Note for any recipe that shells into the Pi: /usr/sbin is NOT on PATH over
# non-interactive ssh. i2cdetect and friends must be called by absolute path.

HOST    ?= sweeney@192.168.1.6
PKGS    := ./...
REMOTE  := /tmp/epaper-hwtest

# Linter, fetched on demand when it is not installed locally. CI pins nothing
# either, so a new check showing up is something to fix rather than to silence.
GOLANGCI         := github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_VERSION := latest
TOOLS_DIR        := $(CURDIR)/.tools
GOLANGCI_BIN     := $(TOOLS_DIR)/golangci-lint

# Long enough to catch a crash introduced since the last run, short enough to
# sit in a pre-push check. Raise it when actually hunting.
FUZZTIME ?= 30s

# The packages holding golden images. Anything else rejects -update.
GOLDEN_PKGS := ./render ./testcard

.DEFAULT_GOAL := check

## test: run the pure packages on this machine, fast
.PHONY: test
test:
	go test -race $(PKGS)

## cover: test with coverage, report per-package
.PHONY: cover
cover:
	go test -race -coverprofile=coverage.out $(PKGS)
	go tool cover -func=coverage.out | tail -n 20

## cover-html: open the coverage report
.PHONY: cover-html
cover-html: cover
	go tool cover -html=coverage.out

## golden: regenerate testdata/golden — REVIEW THE DIFF before committing
#
# Only the packages that import internal/golden define -update, so this cannot
# be $(PKGS): every other package would refuse the flag.
.PHONY: golden
golden:
	go test $(GOLDEN_PKGS) -update
	@echo
	@echo "Goldens regenerated. Review the diff: a golden accepted without"
	@echo "being looked at asserts nothing at all."

## report: run the tests and open an HTML report of the results
.PHONY: report
report:
	@set -o pipefail; go test -race -json -coverprofile=coverage.out $(PKGS) | tee test.json || true
	@go run ./internal/cmd/testreport \
		-json test.json -cover coverage.out -goldens testdata/golden \
		-o test-report.html -title epaper \
		-branch "$$(git rev-parse --abbrev-ref HEAD 2>/dev/null)" \
		-commit "$$(git rev-parse HEAD 2>/dev/null)"
	@echo "open test-report.html"

## lint: vet + golangci-lint, for BOTH host and Linux build constraints
#
# Linting only the host's GOOS silently skips every *_linux.go file, so on a
# Mac the transports are not analysed at all. CI runs on Linux and found six
# issues this way. The linter binary must be native — only the ANALYSIS is
# done as Linux — so this installs one rather than `go run`-ing it, which
# would cross-compile it and fail to exec.
.PHONY: lint
lint: $(GOLANGCI_BIN)
	go vet $(PKGS)
	GOOS=linux GOARCH=arm64 go vet $(PKGS)
	@echo "  linting for $$(go env GOHOSTOS)"
	@$(GOLANGCI_BIN) run $(PKGS)
	@echo "  linting for linux"
	@GOOS=linux GOARCH=arm64 $(GOLANGCI_BIN) run $(PKGS)

# Installed with `go install pkg@version`, which deliberately ignores this
# module's go.mod — building it without the @version would add the linter to
# our dependencies, which is how go.sum grew 413 lines once.
$(GOLANGCI_BIN):
	GOBIN=$(TOOLS_DIR) go install $(GOLANGCI)@$(GOLANGCI_VERSION)

## tidy: go mod tidy must produce no diff
.PHONY: tidy
tidy:
	go mod tidy
	@git diff --exit-code go.mod go.sum 2>/dev/null \
		|| { echo "go.mod/go.sum are not tidy — commit the result of 'go mod tidy'"; exit 1; }

## build: cross-compile for every platform CI builds
.PHONY: build
build:
	@set -e; for t in linux/arm64 linux/arm linux/amd64 darwin/arm64 windows/amd64; do \
		printf "  %-16s" "$$t"; \
		GOOS=$${t%/*} GOARCH=$${t#*/} CGO_ENABLED=0 go build $(PKGS); \
		echo ok; \
	done
	@# 32-bit ARM is what proves the ioctl struct layout for a Pi Zero;
	@# the assertions are compile-time, so building is the test.

## fuzz: a short fuzzing run over the parsers
.PHONY: fuzz
fuzz:
	go test ./inky -run FuzzParseEEPROM -fuzz FuzzParseEEPROM -fuzztime $(FUZZTIME)
	go test . -run FuzzPack -fuzz FuzzPack -fuzztime $(FUZZTIME)

## check: what CI runs — do this before pushing
.PHONY: check
check: tidy lint test build

## test-hw: ship a test binary to the Pi and run it there. No Go toolchain needed on the Pi.
#
# `go test -c` compiles ONE package, which is why every hardware test lives in
# ./hwtest rather than beside the code it exercises.
.PHONY: test-hw
test-hw:
	GOOS=linux GOARCH=arm64 go test -c -tags hardware -o $(REMOTE:%=%.bin) ./hwtest
	scp -q $(REMOTE:%=%.bin) $(HOST):$(REMOTE)
	ssh $(HOST) '$(REMOTE) -test.v; rc=$$?; rm -f $(REMOTE); exit $$rc'
	@rm -f $(REMOTE:%=%.bin)

## testcard: draw the test card on the real panel — ~20 s of refresh
.PHONY: testcard
testcard:
	GOOS=linux GOARCH=arm64 go build -o /tmp/epaper-testcard ./cmd/epaper-testcard
	scp -q /tmp/epaper-testcard $(HOST):/tmp/epaper-testcard
	ssh $(HOST) '/tmp/epaper-testcard; rc=$$?; rm -f /tmp/epaper-testcard; exit $$rc'
	@rm -f /tmp/epaper-testcard

## clean: remove build, coverage and tool output
.PHONY: clean
clean:
	rm -f coverage.out coverage.html test-report.html test.json
	rm -rf $(TOOLS_DIR)
	go clean -testcache

## help: list targets
.PHONY: help
help:
	@grep -hE '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
