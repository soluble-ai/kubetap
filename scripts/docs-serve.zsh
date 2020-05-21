#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

# if file exists and is executable
if [[ -x ./venv/bin/activate ]]; then
  source ./venv/bin/activate
else
  python3 -m venv venv
  source ./venv/bin/activate
  python -m pip install --upgrade pip
  pip install mkdocs mkdocs-material
fi

# build and serve static site
mkdocs serve

deactivate
source ${script_dir}/_post.zsh
