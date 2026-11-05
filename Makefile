GO ?= go
BIN := xbpretty

.PHONY: build test vet fmt completions mirrors clean parity check

build:
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN) ./cmd/xbpretty

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

# Regenerate the shipped completion scripts. The template and
# tools/build-xbps.sh install these instead of running the built binary, which
# a cross build cannot do.
completions:
	$(GO) run ./cmd/xbpretty completion bash > completions/xbpretty.bash
	$(GO) run ./cmd/xbpretty completion zsh  > completions/_xbpretty
	$(GO) run ./cmd/xbpretty completion fish > completions/xbpretty.fish

# Refresh the embedded mirror-list snapshot and its date (FR-076). The release
# manager runs this when the snapshot is older than 90 days.
MIRRORS_URL ?= https://xmirror.voidlinux.org/raw/mirrors.lst
mirrors:
	$(GO) run ./tools/mirrors -url $(MIRRORS_URL)

# The two parity TSVs are the only source. This regenerates Appendix A.2-A.4
# of the spec and docs/parity/matrix.md; the checker refuses a stale rendering.
parity:
	$(GO) run ./tools/parity -write

# Full gate: the parity test rides `go test ./...`, then the explicit check
# proves the committed renderings are current.
check: test
	$(GO) run ./tools/parity -check

clean:
	rm -f $(BIN) xbp
