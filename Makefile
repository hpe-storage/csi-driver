NAME = csi-driver
ifndef REPO_NAME
	REPO_NAME ?= hpestorage/csi-driver
endif

TAG = edge

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
LINTER_FLAGS = --disable-all --enable=vet --enable=vetshadow --enable=golint --enable=ineffassign --enable=goconst --enable=deadcode --enable=dupl --enable=varcheck --enable=gocyclo --enable=misspell --deadline=240s

# Our target binary is for Linux.  To build an exec for your local (non-linux)
# machine, use go build directly.
ifndef GOOS
	GOOS = linux
endif

GOENV = PATH=$$PATH:$(GOPATH)/bin

build: clean compile image push

all: clean lint compile image push

.PHONY: help
help:
	@echo "Targets:"
	@echo "    vendor   - Download dependencies (go mod vendor)"
	@echo "    lint     - Static analysis of source code.  Note that this must pass in order to build."
	@echo "    clean    - Remove build artifacts."
	@echo "    compile  - Compiles the source code."
	@echo "    test     - Run unit tests."
	@echo "    image    - Build csi driver image and create a local docker image.  Errors are ignored."
	@echo "    push     - Push csi driver image to registry."
	@echo "    all      - Clean, lint, build, test, and push image."


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
	@rm -rf build
	@echo "Removing the image"
	-docker image rm $(IMAGE) > /dev/null 2>&1

.PHONY: compile
compile:
	@echo "Compiling the source for ${GOOS}"
	@go version
	@env CGO_ENABLED=0 GOOS=${GOOS} GOARCH=amd64 go build -o build/${NAME} ./cmd/csi-driver/

.PHONY: test
test:
	@echo "Testing all packages"
	@go test -v ./...

# Hack to by pass go mod's inability to pull directories that are not directly referenced
# Note the use of rsync to drop perms, owner, and group on copy
TUNE_LINUX_CONFIG_PATH=$(shell go list -f {{.Dir}} github.com/hpe-storage/common-host-libs/tunelinux/config)
.PHONY: image
image:
	@echo "Building the docker image"

	cd build && \
	cp ../cmd/csi-driver/Dockerfile . && \
	cp ../cmd/csi-driver/rescan-scsi-bus.sh . && \
	cp -r ../cmd/csi-driver/conform/ conform/ && \
	cp -r ../cmd/csi-driver/diag/ diag/ && \
	cp -r ../cmd/csi-driver/chroot-host-wrapper.sh . && \
	cp -r ../LICENSE . && \
	rsync -r --no-perms --no-owner --no-group  $(TUNE_LINUX_CONFIG_PATH)/ tune/ && \
	docker build -t $(IMAGE) .

.PHONY: push
push:
	@echo "Publishing csi-driver:$(VERSION)"
	@docker push $(IMAGE)

