# epaper — see PLAN.md §11.5 for the development loop.
#
# Note for any recipe that shells into the Pi: /usr/sbin is NOT on PATH over
# non-interactive ssh. i2cdetect and friends must be called by absolute path.

# The Pi to talk to, e.g. HOST=pi@raspberrypi.local. There is deliberately no real
# default: there is more than one panel now, so any default would be the wrong
# Pi half the time, and a bench address is not something a public repo should
# carry. require-host below turns a missing one into advice.
HOST    ?= user@your-pi
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
test-hw: require-host
	GOOS=linux GOARCH=arm64 go test -c -tags hardware -o $(REMOTE:%=%.bin) ./hwtest
	scp -q $(REMOTE:%=%.bin) $(HOST):$(REMOTE)
	ssh $(HOST) '$(REMOTE) -test.v; rc=$$?; rm -f $(REMOTE); exit $$rc'
	@rm -f $(REMOTE:%=%.bin)

## testcard: draw a pattern on the real panel — ~20 s of refresh
#
# PATTERN selects what to draw: testcard, orientation or conformance. The
# geometry is NOT a variable here — on real hardware the panel's EEPROM decides
# it, and overriding it would draw something the panel cannot show. Use
# testcard-png for other geometries.
#
#   make testcard HOST=sweeney@pi
#   make testcard HOST=sweeney@pi PATTERN=orientation
PATTERN ?= testcard

.PHONY: testcard
testcard: require-host
	GOOS=linux GOARCH=arm64 go build -o /tmp/epaper-testcard ./cmd/epaper-testcard
	scp -q /tmp/epaper-testcard $(HOST):/tmp/epaper-testcard
	ssh $(HOST) '/tmp/epaper-testcard -pattern $(PATTERN); rc=$$?; rm -f /tmp/epaper-testcard; exit $$rc'
	@rm -f /tmp/epaper-testcard

## testcard-png: render the card at every supported resolution, no hardware
#
# SIZE picks one: a WxH, a model substring, a controller name, or "all"
# (the default). A size nobody sells is allowed — the point is to see what the
# layout does before a driver exists for it.
#
#   make testcard-png                      # every supported panel
#   make testcard-png SIZE=250x122
#   make testcard-png SIZE=pHAT PATTERN=orientation
#
# The card is laid out FROM the panel's size, so "it looks right" is a claim
# about one geometry until it has been looked at on the others. It was
# scrambled at 250x122 for as long as nobody had rendered it there.
SIZE    ?= all
PNGDIR  ?= render-out

.PHONY: testcard-png
testcard-png:
	@go run ./cmd/epaper-testcard -png $(PNGDIR)/$(PATTERN).png -size '$(SIZE)'
	@echo "look at them: $(PNGDIR)/"

## panels: list the panels this library supports
.PHONY: panels
panels:
	@go run ./cmd/epaper-testcard -list

# Fail with advice rather than an opaque ssh error when HOST is still the
# placeholder.
.PHONY: require-host
require-host:
	@test "$(HOST)" != "user@your-pi" || { \
		echo "set HOST to your Pi, e.g. make $(MAKECMDGOALS) HOST=pi@raspberrypi.local"; \
		exit 1; }

## clean: remove build, coverage and tool output
.PHONY: clean
clean:
	rm -f coverage.out coverage.html test-report.html test.json
	rm -rf $(TOOLS_DIR) $(PNGDIR)
	go clean -testcache

## help: list targets
.PHONY: help
help:
	@grep -hE '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
