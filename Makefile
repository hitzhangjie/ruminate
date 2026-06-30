.PHONY: build test lint dev install clean

# Binary name
BINARY := ruminate
BUILD_DIR := build

# Go build flags
LDFLAGS := -s -w

# Default target
all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/ruminate

test:
	go test -v -race -count=1 ./...

lint:
	golangci-lint run ./...

dev:
	# Start backend (build and run)
	@echo "Building backend..."
	go run ./cmd/ruminate serve &
	# Wait for backend to start
	@sleep 1
	# Start frontend dev server
	@echo "Starting frontend dev server..."
	cd web && npm run dev

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/ruminate

clean:
	rm -rf $(BUILD_DIR)

# Run go mod tidy to ensure dependencies are in sync
deps:
	go mod tidy
	cd web && npm install
