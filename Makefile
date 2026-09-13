BIN      := arca
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)
TOOLS    := .tools
AGE_VER  := v1.3.2

.PHONY: build install test check tools lint clean cross

## build: compile the binary (static, no cgo)
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/arca

## install: install into GOBIN
install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/arca

## test: run the unit tests
test:
	go test ./... -race

## tools: fetch the stock age binary used by the emergency-restore tests
tools:
	@mkdir -p $(TOOLS)
	GOBIN=$(CURDIR)/$(TOOLS) go install filippo.io/age/cmd/age@$(AGE_VER)

## check: the full suite, including restoring with stock age/zstd/tar
check: tools lint
	ARCA_TEST_AGE=$(CURDIR)/$(TOOLS)/age go test ./... -race

## lint: vet and formatting
lint:
	go vet ./...
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "gofmt needed"; exit 1)

## cross: prove the binary cross-compiles without cgo
cross:
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -o /dev/null ./cmd/arca
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build -o /dev/null ./cmd/arca
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o /dev/null ./cmd/arca
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/arca

clean:
	rm -rf $(BIN) $(TOOLS)
