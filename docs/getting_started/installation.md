# Installation

## From Source

The recommended installation method is to clone the repository and run:

```sh
$ go generate

$ go install ./cmd/kubectl-tap
```

## Homebrew

Soluble provides a [homebrew formula repository](https://github.com/soluble-ai/homebrew-kubetap)
to use brew to build from source.

```sh
brew tap soluble-ai/homebrew-kubetap

brew install kubetap
```

## From Binary Release

Binary releases for Mac, Windows, and Linux of varying architectures are
available from the [Releases page](https://github.com/soluble-ai/kubetap/releases).

## With Krew

Kubetap can be installed with [krew](https://github.com/kubernetes-sigs/krew):

```sh
kubectl krew install tap
```

