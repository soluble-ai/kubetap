#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

GOTESTSUM_VERSION=v0.4.2

# create a modfile exclusively for CI
print "module ci

go 1.13

require (
)

replace (
  github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.0.0+incompatible
)" >! ci.mod

# install gotestsum, which pretty-formats the test output
#
# uses -modfile=ci.mod to prevent from clobbering the go.mod with CI tooling
# See: https://github.com/golang/go/issues/30515
GO111MODULE=on go get -modfile=ci.mod gotest.tools/gotestsum/@${GOTESTSUM_VERSION}

# test
gotestsum --format=short-verbose --no-summary=skipped --junitfile=coverage.xml -- -count=1 -race -coverprofile=coverage.txt -covermode=atomic ./...

# ensure we can build
go build -v -trimpath -ldflags="-s -w" -o /dev/null ./cmd/kubectl-tap

# tidy modules, in case this is a local build env
go mod tidy

source ${script_dir}/_post.zsh
