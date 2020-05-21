#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

go clean -i ./...
rm -f ./cmd/kubectl-tap/kubeclt-tap
rm -f ./kubectl-tap
rm -rf ./site/

source ${script_dir}/_post.zsh
