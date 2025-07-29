.PHONY: build run test bench clean docker-build docker-run fmt lint

# Variables
BINARY_NAME=marketmaker
DOCKER_IMAGE=lighter-mm:latest
GO_FILES=$(shell find . -name '*.go' -type f)

# Build binary
build:
	go build -o $(BINARY_NAME) -ldflags="-s -w" ./cmd/marketmaker

# Run locally
run: build
	./$(BINARY_NAME) -config configs/config.json

# Run all tests
test:
	go test -v -race ./...

# Run benchmarks
bench:
	go test -bench=. -benchmem ./...

# Format code
fmt:
	go fmt ./...
	goimports -w $(GO_FILES)

# Lint code
lint:
	golangci-lint run

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	go clean -cache

# Docker build
docker-build:
	docker build -t $(DOCKER_IMAGE) .

# Docker run
docker-run: docker-build
	docker-compose up -d

# Docker stop
docker-stop:
	docker-compose down

# View logs
logs:
	docker-compose logs -f marketmaker

# Update dependencies
deps:
	go mod download
	go mod tidy

# Generate mocks
mocks:
	mockery --all --output=mocks

# Run with race detector
race:
	go run -race ./cmd/marketmaker -config configs/config.json

# Profile CPU
profile-cpu:
	go run -cpuprofile=cpu.prof ./cmd/marketmaker -config configs/config.json
	go tool pprof -http=:8080 cpu.prof

# Profile memory
profile-mem:
	go run -memprofile=mem.prof ./cmd/marketmaker -config configs/config.json
	go tool pprof -http=:8080 mem.prof