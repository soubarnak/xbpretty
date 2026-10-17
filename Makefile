GO ?= go
BIN := xbpretty

.PHONY: build test vet fmt completions clean

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

clean:
	rm -f $(BIN) xbp
