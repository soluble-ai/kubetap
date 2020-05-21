#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

GOLANGCI_LINT_VERSION=v1.26.0

# fetch dependencies
go mod download

# if writable file does not exist
if [[ ! -w ci.mod ]]; then
  cp go.mod ci.mod
fi

# install golangci-lint
#
# uses -modfile=ci.mod to prevent from clobbering the go.mod with CI tooling
# See: https://github.com/golang/go/issues/30515
GO111MODULE=on go get -modfile=ci.mod github.com/golangci/golangci-lint/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}

# run golangci-lint
golangci-lint run

source ${script_dir}/_post.zsh
