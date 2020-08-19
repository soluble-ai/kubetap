FROM golang:alpine AS build
WORKDIR $GOPATH/src/github.com/soluble-ai/kubetap
COPY . .
RUN apk add --no-cache -U upx && \
    go mod download && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-w -s" -o /go/bin/kubectl-tap ./cmd/kubectl-tap && \
    upx /go/bin/kubectl-tap

FROM alpine:latest as alpine
WORKDIR /usr/share/zoneinfo
RUN apk -U --no-cache add tzdata zip ca-certificates && \
    zip -r -0 /zoneinfo.zip .

FROM scratch
WORKDIR /app
COPY --from=build /go/bin/kubectl-tap .
ENV ZONEINFO /zoneinfo.zip
COPY --from=alpine /zoneinfo.zip /
COPY --from=alpine /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["./kubectl-tap"]
