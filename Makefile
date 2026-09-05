.PHONY: build test race vet clean fmt

# Binary output
BINARY := scalpbot
BUILD_DIR := bin

# Build flags
LDFLAGS := -ldflags="-s -w"
GCFLAGS_DEBUG := -gcflags="-l"

## build: Compile production binary with stripped symbols
build:
	@echo "==> Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY).exe ./cmd/bot/

## build-debug: Compile with inlining disabled for debugging
build-debug:
	@echo "==> Building $(BINARY) (debug)..."
	@mkdir -p $(BUILD_DIR)
	go build $(GCFLAGS_DEBUG) -o $(BUILD_DIR)/$(BINARY)-debug.exe ./cmd/bot/

## test: Run all unit tests with verbose output
test:
	@echo "==> Running tests..."
	go test -v -count=1 ./...

## race: Run tests with Go race detector enabled
race:
	@echo "==> Running tests with race detector..."
	go test -v -race -count=1 ./...

## vet: Run Go static analysis
vet:
	@echo "==> Running go vet..."
	go vet ./...

## fmt: Format all Go source files
fmt:
	@echo "==> Formatting code..."
	gofmt -s -w .

## clean: Remove build artifacts
clean:
	@echo "==> Cleaning..."
	@rm -rf $(BUILD_DIR)

## bench: Run benchmarks
bench:
	@echo "==> Running benchmarks..."
	go test -bench=. -benchmem ./...

## help: Show available targets
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
