//go:generate ./scripts/deps.sh
//go:generate go clean -i ./...
//go:generate rm -f ./cmd/kubectl-tap/kubectl-tap
//go:generate go mod download
//go:generate golangci-lint run
//go:generate gotestsum --format=short-verbose --no-summary=skipped --junitfile=coverage.xml -- -count=1 -race -coverprofile=coverage.txt -covermode=atomic ./...
package main

func main() {}
