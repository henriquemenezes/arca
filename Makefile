BIN      := arca
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)
TOOLS    := .tools
AGE_VER  := v1.3.2
LINT_VER := v2.14.0
VULN_VER := v1.8.0
GREL_VER := v2.18.2

DIST     := dist

.PHONY: help build install test test-upstream check tools lint fmt vuln clean cross snapshot release-check licenses

## help: list the targets in this file
help:
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | awk -F': ' '{printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}'

## build: compile the binary (static, no cgo)
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/arca

## install: install into GOBIN
install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/arca

## test: run the unit tests
test:
	go test ./... -race

## tools: build the age binary used by the emergency-restore tests
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

## test-upstream: the tests plus the restore path that uses real age/zstd/tar
test-upstream: tools
	ARCA_TEST_AGE=$(CURDIR)/$(TOOLS)/age go test ./... -race -timeout 20m

## check: everything CI runs
check: lint vuln test-upstream

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
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm GOARM=7 go build -o /dev/null ./cmd/arca
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o /dev/null ./cmd/arca
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/arca

## snapshot: build every release artefact locally, without needing a tag
# Signing is always skipped here: keyless cosign proves that the *workflow*
# produced the artefact, so signing from a laptop would prompt a browser and
# record the wrong identity. SBOMs need syft, which CI installs as a binary
# rather than compiling, so they are skipped unless it is on PATH.
snapshot: $(TOOLS)/goreleaser
	@command -v syft >/dev/null || echo "  syft not on PATH: skipping SBOMs"
	$(TOOLS)/goreleaser release --snapshot --clean --skip=sign \
		$$(command -v syft >/dev/null || echo --skip=sbom)

## release-check: validate .goreleaser.yaml
release-check: $(TOOLS)/goreleaser
	$(TOOLS)/goreleaser check

$(TOOLS)/goreleaser:
	@mkdir -p $(TOOLS)
	GOBIN=$(CURDIR)/$(TOOLS) go install github.com/goreleaser/goreleaser/v2@$(GREL_VER)

## licenses: regenerate the third-party notice file attached to releases
licenses:
	./scripts/third-party-licenses.sh THIRD_PARTY_LICENSES.md

clean:
	rm -rf $(BIN) $(TOOLS) $(DIST) THIRD_PARTY_LICENSES.md
