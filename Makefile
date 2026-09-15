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
.PHONY: golden
golden:
	go test ./render -update
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
		-commit "$$(git rev-parse --short HEAD 2>/dev/null)"
	@echo "open test-report.html"

## lint: vet + golangci-lint (downloaded on demand if not installed)
.PHONY: lint
lint:
	go vet $(PKGS)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run $(PKGS); \
	else \
		echo "golangci-lint not installed; running it via go run"; \
		GOFLAGS=-mod=mod go run $(GOLANGCI)@$(GOLANGCI_VERSION) run $(PKGS); \
	fi

## tidy: go mod tidy must produce no diff
.PHONY: tidy
tidy:
	go mod tidy
	@git diff --exit-code go.mod go.sum 2>/dev/null \
		|| { echo "go.mod/go.sum are not tidy — commit the result of 'go mod tidy'"; exit 1; }

## build: cross-compile for the Pi (and check the mock path stays portable)
.PHONY: build
build:
	GOOS=linux GOARCH=arm64 go build $(PKGS)
	GOOS=linux GOARCH=arm   go build $(PKGS)
	GOOS=darwin GOARCH=arm64 go build $(PKGS)

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

## testcard: draw Test Card F on the real panel — ~25 s of refresh
.PHONY: testcard
testcard:
	GOOS=linux GOARCH=arm64 go build -o /tmp/epaper-testcard ./cmd/epaper-testcard
	scp -q /tmp/epaper-testcard $(HOST):/tmp/epaper-testcard
	ssh $(HOST) '/tmp/epaper-testcard; rc=$$?; rm -f /tmp/epaper-testcard; exit $$rc'
	@rm -f /tmp/epaper-testcard

## clean: remove build and coverage output
.PHONY: clean
clean:
	rm -f coverage.out coverage.html
	go clean -testcache

## help: list targets
.PHONY: help
help:
	@grep -hE '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
