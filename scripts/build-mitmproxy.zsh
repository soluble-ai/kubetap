#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

image="kubetap-mitmproxy:dev"

cd ./proxies/mitmproxy/
docker build --pull --no-cache -t ${image} .

source ${script_dir}/_post.zsh
