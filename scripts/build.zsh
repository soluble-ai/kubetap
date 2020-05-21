#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

source ${script_dir}/lint.zsh
source ${script_dir}/test.zsh
# Cleaning before final build
source ${script_dir}/clean.zsh
source ${script_dir}/build-kubetap.zsh

# tidy modules
go mod tidy

source ${script_dir}/_post.zsh
