#!/usr/bin/env zsh
# hide repetitive output
set +x
# formate the prompt for sourced files
PS4='%F{magenta}+%2N%f:%F{green}%I%f    '
# store the current PWD
_kubetap_PWD=${PWD}
if [[ ! -z _kubetap_PWD ]]; then
  # this is a nested _pre.zsh call
  _kubetap_nested=true
fi
# move into the same directory as the script
cd ${0:h}
# and get the parent .git directory 
_kubetap_git_root=$(command git rev-parse --git-dir) || return 1
# and cd to the parent of the .git directory
cd ${_kubetap_git_root:h}
set -eux
