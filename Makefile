ZSH := $(shell command -v zsh 2>/dev/null)

.PHONY: all
all: zsh clean build

# build.zsh is our meta-build script. Individual scripts
# are also provided as make targets for developer convenience.
.PHONY: build
build: zsh
	./scripts/build.zsh

.PHONY: clean
clean: zsh
	./scripts/clean.zsh

.PHONY: test
test: unit-test ig-test

.PHONY: unit-test
unit-test: zsh
	./scripts/test.zsh

.PHONY: ig-test
ig-test: zsh
	./scripts/ig-test.zsh

.PHONY: lint
lint: zsh
	./scripts/lint.zsh

.PHONY: images
images: zsh
	./scripts/images.zsh

.PHONY: docs-build
docs-build: zsh
	./scripts/docs-build.zsh

.PHONY: docs
docs: zsh
	./scripts/docs-serve.zsh

.PHONY: zsh
zsh:
ifndef ZSH
	$(error "zsh is not available. Install zsh or do a manual go build")
endif

.PHONY: help
help:
	cat Makefile
