GO ?= go
BIN := xbpretty

.PHONY: build test vet fmt clean

build:
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN) ./cmd/xbpretty

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

clean:
	rm -f $(BIN) xbp
