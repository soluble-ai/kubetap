#!/usr/bin/env zsh
set +x

# cd to the stored pwd
cd ${_kubetap_PWD}
# and unset our variables if this isn't a nested _post calls
if [[ _kubetap_nested ]]; then
  unset _kubetap_nested PS4
  return
fi
unset _kubetap_git_root _kubetap_PWD script_dir PS4
