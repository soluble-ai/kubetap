# Installation

## From Source

The recommended installation method is to clone the repository and run:

```sh
make
```

Kubetap can also be installed from source using the following one-liner:

```sh
cd && GO111MODULE=on go get github.com/soluble-ai/kubetap/cmd/kubectl-tap@latest
```

## Brew

Soluble provides a [homebrew formula repository](https://github.com/soluble-ai/homebrew-kubetap)
to use brew to build from source.

```sh
brew tap soluble-ai/homebrew-kubetap

brew install kubetap
```

## From Binary Release

Binary releases for Mac, Windows, and Linux of varying architectures are
available from the [Releases page](https://github.com/soluble-ai/kubetap/releases).

