GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
GORELEASER    := go run github.com/goreleaser/goreleaser/v2@v2.18.0

.PHONY: all fmt vet lint test test-race build check release-check clean

all: check

fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run ./...

test:
	go test ./...

# Requires cgo (a C compiler). Not available on the Windows dev box; CI runs it on ubuntu/macos.
test-race:
	go test -race ./...

build:
	go build -o skope$(EXE) ./cmd/skope

# Everything that must be green before a phase is considered complete (spec §14.7).
check: fmt vet lint test build

release-check:
	$(GORELEASER) check

clean:
	rm -f skope skope.exe coverage.txt
	rm -rf dist

ifeq ($(OS),Windows_NT)
EXE := .exe
endif
