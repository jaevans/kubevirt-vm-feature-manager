.PHONY: help build test test-verbose clean lint fmt vet docker-build generate-mocks release release-snapshot

# Variables
BINARY_NAME=webhook
DOCKER_IMAGE?=vm-feature-manager
DOCKER_TAG?=latest
GO=go
GINKGO=ginkgo
GOLANGCI_LINT=golangci-lint
MOCKERY=mockery
GORELEASER=goreleaser

# Get the repository owner from git remote (for local builds)
GITHUB_REPOSITORY_OWNER?=$(shell git remote get-url origin 2>/dev/null | sed -n 's#.*github\.com[:/]\([^/]*\)/.*#\1#p' || echo "jaevans")

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

# Setup envtest binaries
ENVTEST_K8S_VERSION = 1.33.0
ENVTEST_ASSETS_DIR=$(shell setup-envtest use $(ENVTEST_K8S_VERSION) -p path)

build: ## Build the webhook binary
	$(GO) build -o $(BINARY_NAME) cmd/webhook/main.go

test: ## Run unit tests
	$(GINKGO) -r --skip-package=test/integration

test-integration: ## Run integration tests
	KUBEBUILDER_ASSETS="$(ENVTEST_ASSETS_DIR)" $(GINKGO) test/integration

test-all: ## Run all tests (unit + integration)
	$(MAKE) test
	$(MAKE) test-integration

test-verbose: ## Run tests with verbose output
	$(GINKGO) -r -v --skip-package=test/integration

test-coverage: ## Run tests with coverage
	$(GINKGO) -r --cover --coverprofile=coverage.out --skip-package=test/integration
	$(GO) tool cover -html=coverage.out -o coverage.html

test-coverage-all: ## Run all tests (including integration) with coverage
	KUBEBUILDER_ASSETS="$(ENVTEST_ASSETS_DIR)" $(GINKGO) -r --cover --coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html

watch: ## Run tests in watch mode
	$(GINKGO) watch -r --skip-package=test/integration

fmt: ## Run go fmt
	$(GO) fmt ./...

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

lint-fix: ## Run golangci-lint with auto-fix
	$(GOLANGCI_LINT) run --fix ./...

tidy: ## Tidy go modules
	$(GO) mod tidy

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -rf dist/

##@ Build

docker-build: ## Build Docker image (traditional)
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-push: ## Push Docker image (traditional)
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

release-snapshot: ## Build a snapshot release locally (no push)
	GITHUB_REPOSITORY_OWNER=$(GITHUB_REPOSITORY_OWNER) $(GORELEASER) release --snapshot --clean --skip=sbom,sign

release-test: ## Test the release process without publishing
	GITHUB_REPOSITORY_OWNER=$(GITHUB_REPOSITORY_OWNER) $(GORELEASER) release --skip=publish --clean

release: ## Create a release (use with git tags)
	GITHUB_REPOSITORY_OWNER=$(GITHUB_REPOSITORY_OWNER) $(GORELEASER) release --clean

release-dry-run: ## Validate the release configuration
	GITHUB_REPOSITORY_OWNER=$(GITHUB_REPOSITORY_OWNER) $(GORELEASER) check

##@ Code Generation

generate: ## Run code generators
	$(GO) generate ./...

generate-mocks: ## Generate mocks using mockery
	$(MOCKERY)

install-tools: ## Install development tools
	$(GO) install github.com/onsi/ginkgo/v2/ginkgo@latest
	$(GO) install github.com/vektra/mockery/v2@latest
	@echo "Installing goreleaser..."
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "Downloading goreleaser binary..."; \
		GOBIN=$(shell go env GOPATH)/bin go install github.com/goreleaser/goreleaser/v2@latest || \
		echo "Warning: 'go install' failed, trying binary download..."; \
		mkdir -p /tmp/goreleaser && cd /tmp/goreleaser && \
		curl -sL https://github.com/goreleaser/goreleaser/releases/latest/download/goreleaser_Linux_x86_64.tar.gz | tar xz && \
		mv goreleaser $(shell go env GOPATH)/bin/ && \
		echo "goreleaser installed to $(shell go env GOPATH)/bin/goreleaser" || \
		echo "Failed to install goreleaser. Install manually: https://goreleaser.com/install/"; \
	else \
		echo "goreleaser already installed at $$(which goreleaser)"; \
	fi
	@echo "Installing ko..."
	@if ! command -v ko >/dev/null 2>&1; then \
		echo "Installing ko via go install..."; \
		GOBIN=$(shell go env GOPATH)/bin $(GO) install github.com/google/ko@latest && \
		echo "ko installed to $(shell go env GOPATH)/bin/ko" || \
		echo "Failed to install ko. Install manually: https://ko.build/install/"; \
	else \
		echo "ko already installed at $$(which ko)"; \
	fi
	@echo "Development tools installed"

setup-envtest: ## Install setup-envtest for integration tests
	$(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	@echo "setup-envtest installed"
