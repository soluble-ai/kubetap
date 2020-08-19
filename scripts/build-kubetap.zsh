#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

# Go modules still are not great, and twice now I've had a broken mod
# cache. If others experience this, I will enforce a clean cache for
# all builds. I really don't want to do that.
#go clean -modcache

# downloading deps
go mod download

# build and install
go install -v -trimpath -ldflags="-s -w" ./cmd/kubectl-tap

source ${script_dir}/_post.zsh
