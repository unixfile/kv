# Build: make
# Test: make test
# Install: make install (or: go install github.com/unixfile/kv/cmd/kv@latest)
# Docker: docker build -t kv . && docker run --rm -i kv json < file.kv

.PHONY: all test install clean

all: kv

kv: $(wildcard *.go cmd/kv/*.go) go.mod
	go build -o $@ ./cmd/kv

test:
	go test ./...

install:
	go install ./cmd/kv

clean:
	$(RM) kv
