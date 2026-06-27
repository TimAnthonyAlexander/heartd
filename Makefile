BINARY := heartd
PKG := ./cmd/heartd
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# Build version injected into the binary (used e.g. for the health-check
# User-Agent). Falls back to the package default ("dev") when git is absent.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/timanthonyalexander/heartd/internal/version.Version=$(VERSION)

.PHONY: all build frontend backend dev test clean cross

# Full release build: frontend bundle embedded into the Go binary.
all: build

frontend:
	cd frontend && bun install && bun run build

backend:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

build: frontend backend

# Build the binary using whatever is already in internal/web/dist.
backend-only: backend

# Run the Go server (serves embedded dist on :9300).
run: backend
	./$(BINARY)

# Split dev mode: start Go API, then Vite dev server with HMR + proxy.
dev:
	@echo "Run in two terminals:"
	@echo "  1) go run $(PKG)"
	@echo "  2) cd frontend && bun run dev   # http://localhost:5173"

test:
	go test ./...

clean:
	rm -f $(BINARY)
	rm -rf frontend/dist

# Cross-compile static binaries for all target platforms into bin/.
cross: frontend
	@mkdir -p bin
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-$$os-$$arch $(PKG); \
	done
