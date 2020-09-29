#!/bin/sh

GOLANGCI_LINT_VERSION=v1.26.0
GOTESTSUM_VERSION=v0.4.2
KIND_VERSION=v0.8.1
HELM_VERSION=v3.2.1

cd

if ! [ -x "$(command -v kubectl)" ]; then
  echo "kubectl is not installed"
  exit 1
fi


if ! [ -x "$(command -v golangci-lint)" ]; then
  GO111MODULE=on go get github.com/golangci/golangci-lint/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}
fi

if ! [ -x "$(command -v golangci-lint)" ]; then
  GO111MODULE=on go get gotest.tools/gotestsum/@${GOTESTSUM_VERSION}
fi

if ! [ -x "$(command -v helm)" ]; then
  GO111MODULE=on go get helm/cmd/helm@${HELM_VERSION}
fi


if ! [ -x "$(command -v kind)" ]; then
  GO111MODULE=on go get sigs.k8s.io/kind@${KIND_VERSION}
fi

if ! [ -x "$(command -v gofumpt)" ]; then
  GO111MODULE=on go get mvdan.cc/gofumpt
fi

if ! [ -x "$(command -v gofumports)" ]; then
  GO111MODULE=on go get mvdan.cc/gofumpt/gofumports
fi



cd -
