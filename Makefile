BINARY  := chatmux
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

.PHONY: install
install: ## Install the binary into GOPATH/bin
	go install -ldflags "$(LDFLAGS)" .

.PHONY: test
test: ## Run the tests
	go test ./...

.PHONY: race
race: ## Run the tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run the tests and open the coverage report
	go test -coverprofile=coverage.txt ./...
	go tool cover -html=coverage.txt

.PHONY: fmt
fmt: ## Format the code
	gofmt -s -w .

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint (must be installed)
	golangci-lint run

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: check
check: vet test ## Run vet and tests

.PHONY: clean
clean: ## Remove build output
	rm -f $(BINARY) coverage.txt
	rm -rf dist/
