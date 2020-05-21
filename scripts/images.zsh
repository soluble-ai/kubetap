#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

# perform lossless compression of images
cd ./docs/img/ && \
  optipng *.png && \
  exiftool -all= *.png

source ${script_dir}/_post.zsh
