.PHONY: build dev test lint install deps clean

BINARY := ruminate
BUILD_DIR := build
LDFLAGS := -s -w

# Default: build UI + binary
build:
	cd web && npm run build
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/ruminate

# Build then serve (API + UI)
dev: build
	./$(BUILD_DIR)/$(BINARY) serve

test:
	go test -v -race -count=1 ./...

lint:
	golangci-lint run ./...

install: build
	go install -ldflags "$(LDFLAGS)" ./cmd/ruminate

deps:
	go mod tidy
	cd web && npm install

clean:
	rm -rf $(BUILD_DIR)
