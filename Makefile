NAME = csi-driver
COVER_PROFILE := $(NAME).cov

CI_MINIMUM_TEST_COVERAGE := 27
TEST_MODULES := ./...

GO      := go

ifndef REPO_NAME
	REPO_NAME ?= hpestorage/csi-driver
endif

ifndef TAG
	TAG ?= edge
endif

ARCH ?= amd64

# unless a BUILD_NUMBER is specified
ifeq ($(IGNORE_BUILD_NUMBER),true)
	VERSION = $(TAG)
else
	ifneq ($(BUILD_NUMBER),)
		VERSION = $(TAG)-$(BUILD_NUMBER)
	else
		VERSION = $(TAG)
	endif
endif

# refers to dockerhub if registry is not specified
IMAGE = $(REPO_NAME):$(VERSION)
ifdef CONTAINER_REGISTRY
	IMAGE = $(CONTAINER_REGISTRY)/$(REPO_NAME):$(VERSION)
endif

# golangci-lint allows us to have a single target that runs multiple linters in
# the same fashion.  This variable controls which linters are used.
LINTER_FLAGS = --disable-all --enable=govet --enable=revive --enable=ineffassign --enable=goconst --enable=unused --enable=dupl --enable=gocyclo --enable=misspell

# Our target binary is for Linux.  To build an exec for your local (non-linux)
# machine, use go build directly.
ifndef GOOS
	GOOS = linux
endif

GOENV = PATH=$$PATH:$(GOPATH)/bin

build: clean compile unit-test image push

all: clean lint compile unit-test image push

.PHONY: help
help:
	@echo "Targets:"
	@echo "    vendor     - Download dependencies (go mod vendor)"
	@echo "    lint       - Static analysis of source code.  Note that this must pass in order to build."
	@echo "    clean      - Remove build artifacts."
	@echo "    compile    - Compiles the source code."
	@echo "    unit-test  - Run unit tests."
	@echo "    image      - Build csi driver image and create a local docker image.  Errors are ignored."
	@echo "    push       - Push csi driver image to registry."
	@echo "    all        - Clean, lint, build, test, and push image."


vendor:
	@go mod vendor

.PHONY: lint
lint:
	@echo "Running lint"
	@go version
	export $(GOENV) && golangci-lint run $(LINTER_FLAGS) --exclude vendor

.PHONY: clean
clean:
	@echo "Removing build artifacts"
	@rm -rf build $(COVER_PROFILE) unit-test-result.json c.out coverage.html unit_test_coverage.html

.PHONY: compile
compile:
	@echo "Compiling the source for ${GOOS}"
	@go version
	@env CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${ARCH} go build -o build/${NAME} ./cmd/csi-driver/

.PHONY: image
image:
	@echo "Building multiarch docker images and manifest"
	docker-buildx build --platform=linux/amd64,linux/arm64 --progress=plain \
		--provenance=false -t $(IMAGE) .

.PHONY: push
push:
	@echo "Publishing $(IMAGE)"
	docker-buildx build --platform=linux/amd64,linux/arm64 --progress=plain \
		--provenance=false --push -t $(IMAGE) .

.PHONY: unit-test
unit-test: vendor # mocks
	$(GO) test $(TEST_MODULES) \
		-timeout 30m \
		-v \
		-cover \
		-coverprofile=c.out \
		-count=1 \
                -json \
                > unit-test-result.json || (jq . < unit-test-result.json; false)
	cat c.out | grep -v "/external/" | grep -v "/mocks/" > $(COVER_PROFILE)

## code-coverage: Produce a per-function code coverage report from a unit test run.
.PHONY: code-coverage
code-coverage: $(COVER_PROFILE)
	$(GO) tool cover -func $(COVER_PROFILE)
	$(GO) tool cover -html=$(COVER_PROFILE) -o unit_test_coverage.html
	$(eval TEST_COVERAGE = $(shell $(GO) tool cover -func $(COVER_PROFILE) | grep 'total:' | awk '{print substr($$3, 1, length($$3)-1)}'))
	@echo "Unit-tests passed with $(TEST_COVERAGE) coverage"
	$(eval PASSED_COVERAGE = $(shell awk 'BEGIN {printf ($(TEST_COVERAGE) < ${CI_MINIMUM_TEST_COVERAGE} ? "0" : "1")}'))
	@if [ $(PASSED_COVERAGE) == 0 ]; then echo "Require at least ${CI_MINIMUM_TEST_COVERAGE}% test coverage"; exit 1; fi

## trivy: Run trivy vulnerability scanner.
.PHONY: trivy
trivy:
	@echo 'NOTE: To suppress failures (with reason) see .trivyignore.'
	trivy fs --severity UNKNOWN,LOW,MEDIUM --no-progress --scanners vuln .
	trivy fs --exit-code 1 --severity HIGH,CRITICAL --no-progress --scanners vuln .
