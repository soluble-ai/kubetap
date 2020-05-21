#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

# if file exists and is executable
if [[ -x ./venv/bin/activate ]]; then
  source ./venv/bin/activate
else
  python3 -m venv venv
  source ./venv/bin/activate
  python -m pip install --upgrade pip setuptools wheel
  pip install mkdocs mkdocs-material
fi

# build docs, but don't serve.
# docs are avaliable as HTML in ./site/
mkdocs build

deactivate
source ${script_dir}/_post.zsh
