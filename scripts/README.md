# Scripts

Scripts to manage the project. They can also be invoked from the Makefile
at the project's root.

All scripts require Zsh. [Variable modifiers](http://zsh.sourceforge.net/Doc/Release/Expansion.html#Modifiers)
are too useful to consciously choose to use `/bin/sh` like a caveman. A recent
(enough) version of zsh installed in Macs by default, and every distro has a zsh
package.

| Script                | Purpose                                                             |
| ---                   | ---                                                                 |
| `build.zsh`           | meta build script, excluding container builds and integration tests |
| `build-mitmproxy.zsh` | builds the mitmproxy container                                      |
| `build-kubetap.zsh`   | builds the kubectl-tap binary                                       |
| `docs-build.zsh`      | builds the static files for gh-pages                                |
| `docs-serve.zsh`      | interactive local docs served by mkdocs                             |
| `images.zsh`          | strip metadata and perform lossless compression                     |
| `test.zsh`            | unit tests                                                          |
| `ig-test.zsh`         | integration tests                                                   |
| `_pre.zsh`            | handles output formatting and PWD storage                           |
| `_post.zsh`           | restores PWD state from `pre.zsh`                                   |
