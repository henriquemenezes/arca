BIN      := arca
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)
TOOLS    := .tools
AGE_VER  := v1.3.2
LINT_VER := v2.14.0
VULN_VER := v1.8.0

.PHONY: build install test test-stock check tools lint fmt vuln clean cross

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
tools: $(TOOLS)/age

$(TOOLS)/age:
	@mkdir -p $(TOOLS)
	GOBIN=$(CURDIR)/$(TOOLS) go install filippo.io/age/cmd/age@$(AGE_VER)

$(TOOLS)/golangci-lint:
	@mkdir -p $(TOOLS)
	GOBIN=$(CURDIR)/$(TOOLS) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VER)

$(TOOLS)/govulncheck:
	@mkdir -p $(TOOLS)
	GOBIN=$(CURDIR)/$(TOOLS) go install golang.org/x/vuln/cmd/govulncheck@$(VULN_VER)

## test-stock: the tests plus the restore path that uses stock age/zstd/tar
test-stock: tools
	ARCA_TEST_AGE=$(CURDIR)/$(TOOLS)/age go test ./... -race -timeout 20m

## check: everything CI runs
check: lint vuln test-stock

## lint: static analysis (vet, staticcheck, errcheck, gosec, ...) and formatting
lint: $(TOOLS)/golangci-lint
	$(TOOLS)/golangci-lint run
	$(TOOLS)/golangci-lint fmt --diff

## fmt: apply gofmt and goimports in place
fmt: $(TOOLS)/golangci-lint
	$(TOOLS)/golangci-lint fmt

## vuln: report CVEs this code actually reaches
vuln: $(TOOLS)/govulncheck
	$(TOOLS)/govulncheck ./...

## cross: prove the binary cross-compiles without cgo
cross:
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -o /dev/null ./cmd/arca
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build -o /dev/null ./cmd/arca
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o /dev/null ./cmd/arca
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/arca

clean:
	rm -rf $(BIN) $(TOOLS)
