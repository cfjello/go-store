# Simple Makefile for a Go project

# Build the application
all: build test

build:
	@echo "Building..."
	
	
	@go build -o main.exe cmd/api/main.go

# Run the application
run:
	@go run cmd/api/main.go

# Test the application
test:
	@echo "Testing..."
	@go test ./... -v

# Clean the binary
clean:
	@echo "Cleaning..."
	@-del /f /q main.exe 2>NUL & rm -f main.exe main

# Vet the code
vet:
	@go vet ./...

# Lint the code (requires golangci-lint)
lint:
	@powershell -ExecutionPolicy Bypass -Command "if (Get-Command golangci-lint -ErrorAction SilentlyContinue) { \
		golangci-lint run; \
	} else { \
		Write-Output 'golangci-lint not found, skipping lint...'; \
	}"

# Tidy module dependencies
tidy:
	@go mod tidy

# Test with coverage report
coverage:
	@go test ./... -coverprofile=coverage.out
	@go tool cover -func=coverage.out

# Live Reload
watch:
	@powershell -ExecutionPolicy Bypass -Command "if (Get-Command air -ErrorAction SilentlyContinue) { \
		air; \
		Write-Output 'Watching...'; \
	} else { \
		Write-Output 'Installing air...'; \
		go install github.com/air-verse/air@latest; \
		air; \
		Write-Output 'Watching...'; \
	}"

.PHONY: all build run test clean vet lint tidy coverage watch
