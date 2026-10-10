CONTAINER ?= docker
IMAGE ?= aiu-dev
VERSION ?= dev
UID := $(shell id -u)
GID := $(shell id -g)
CACHE_DIR ?= $(if $(XDG_CACHE_HOME),$(XDG_CACHE_HOME),$(HOME)/.cache)/aiu-build
# Credentials never enter the build container. Only the repository is mounted.
RUN = $(CONTAINER) run --rm --user $(UID):$(GID) \
	-v "$(CURDIR):/workspace" -w /workspace \
	-v "$(CACHE_DIR):/go-cache" -e GOCACHE=/go-cache \
	-e HOME=/tmp -e AIU_BUILD_VERSION="$(VERSION)" $(IMAGE)

.PHONY: image build test fmt check devcontainer
image:
	mkdir -p "$(CACHE_DIR)"
	$(CONTAINER) build --target dev -t $(IMAGE) .

build: image
	$(RUN) sh -c 'mkdir -p dist && go build -trimpath -ldflags="-s -w -X main.version=$$AIU_BUILD_VERSION" -o dist/aiu ./cmd/aiu'

test: image
	$(RUN) go test ./...

fmt: image
	$(RUN) sh -c 'find cmd internal -name "*.go" -exec gofmt -w {} +'

check: image
	$(RUN) sh -c 'test -z "$$(gofmt -l cmd internal)" && sh -n install.sh && go vet ./... && go test ./...'
