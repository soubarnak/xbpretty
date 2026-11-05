GO ?= go
BIN := xbpretty

.PHONY: build test vet fmt completions manpages mirrors clean parity check release-check

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

# Regenerate the shipped manual pages and the six man8 alias links. Both pages
# derive from internal/spec, like the completions, so a command or flag change
# appears everywhere at once.
manpages:
	$(GO) run ./tools/manpages

# Refresh the embedded mirror-list snapshot and its date (FR-076). The release
# manager runs this when the snapshot is older than 90 days.
MIRRORS_URL ?= https://xmirror.voidlinux.org/raw/mirrors.lst
mirrors:
	$(GO) run ./tools/mirrors -url $(MIRRORS_URL)

# The two parity TSVs are the only source. This regenerates Appendix A.2-A.4
# of the spec and docs/parity/matrix.md; the checker refuses a stale rendering.
parity:
	$(GO) run ./tools/parity -write

# Release path: the generated completions and manpages must be committed and
# current, the shell dialects and mandoc must accept them, and the template and
# build script install the same surface. A drift fails here and in `go test`.
release-check: test
	$(GO) test ./internal/completion -run 'TestShippedScriptsAreCurrent|TestCompletionScriptsPassShellSyntax' -count=1
	$(GO) test ./internal/manpage -run 'TestGeneratedManpagesAreCurrent|TestAliasMan8LinksResolve|TestManpagesPassMandocLint' -count=1
	bash -n completions/xbpretty.bash
	if command -v zsh >/dev/null 2>&1; then zsh -n completions/_xbpretty; fi
	if command -v fish >/dev/null 2>&1; then fish -n completions/xbpretty.fish; fi
	if command -v mandoc >/dev/null 2>&1; then mandoc -T lint man/xbpretty.8 man/xbpretty.conf.5; fi
	$(GO) run ./tools/parity -check

# Full gate: parity, generated-artifact freshness and shell/man lint.
check: release-check

clean:
	rm -f $(BIN) xbp
