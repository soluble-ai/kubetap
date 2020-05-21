# Kubetap Development

This section provides information about Kubetap development processes and how
to contribute.

## Kubetap overview

Kubtap is developed by [Matt Hamilton](https://github.com/eriner) at [Soluble.ai](https://www.soluble.ai/) as an [Apache v2 Licensed](https://github.com/soluble-ai/kubetap/blob/master/LICENSE)
open source project.

Kubetap arose from a need to quickly and efficiently proxy Kubernetes Services
without imposing a CNI mandate.

Building kubetap requires the following dependencies:

| dependency | purpose               | notes                                         |
| ---        | ---                   | ---                                           |
| `kubectl`  | ...                   | mandatory for integration tests               |
| `docker`   | Build containers      | not needed to build `kubectl-tap` binary      |
| `go`       | Build `kubectl-tap`   | minimum Go version 1.13                       |
| `zsh`      | Build scripts         | scripting is nicer than `bash` or `sh`        |
| `make`     | Developer convenience | not explicit dependency, just invokes scripts |

Script-managed dependencies, installed using `go get` and `ci.mod` or `ig-tests.mod`:

| dependency      | purpose | notes                                                      |
| ---             | ---     | ---                                                        |
| `golangci-lint` | Linting | (`ci.mod`) used as Go code linter                          |
| `gotestsum`     | Testing | (`ci.mod`) used to make test output prettier               |
| `kind`          | Testing | (`ig-tests.mod`) required for integration tests            |
| `helm`          | Testing | (`ig-tests.mod`) used to deploy test apps to  kind cluster |

## Hacking on Kubetap

Assuming you have [built kubetap from source](../getting_started/installation.md),
you're ready to hack on kubetap.

```sh
$ cd ${GOPATH}/src/github.com/soluble-ai/kubetap

$ go mod download

$ go build ./cmd/kubectl-tap
```

There is a [build script](https://github.com/soluble-ai/kubetap/blob/master/scripts/build.zsh)
which downloads Go modules, runs a linter, and runs unit tests prior to building
the binary. This script can be invoked manually or with the `Makefile`:

```sh
make
```

Integration tests, which can take considerably longer to complete, should be run
prior to submitting a pull request to ensure that the PR doesn't break functionality:

```sh
make test
```
