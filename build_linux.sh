#!/bin/sh
set -eu

archive=$1
project=$2
build_root=$(mktemp -d /tmp/cpa-privacy-build-XXXXXXXX)
cleanup() {
    case "$build_root" in /tmp/cpa-privacy-build-*) rm -rf -- "$build_root" ;; *) exit 1 ;; esac
}
trap cleanup EXIT INT TERM

tar -xzf "$archive" -C "$build_root"
mkdir "$build_root/src"
cp "$project/go.mod" "$project/normalizer.go" "$project/normalizer_test.go" "$project/bridge.go" "$build_root/src/"
export GOROOT="$build_root/go"
export GOCACHE="$build_root/cache"
export GOENV=off
export GOPROXY=off
export GOSUMDB=off
export PATH="$GOROOT/bin:$PATH"
cd "$build_root/src"
go version
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 CC=gcc go test -race ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 CC=gcc go vet ./...
mkdir -p "$project/dist/linux-arm64"
GOOS=linux GOARCH=arm64 CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc \
    go build -trimpath -buildmode=c-shared -ldflags='-s -w' \
    -o "$project/dist/linux-arm64/privacy-redactor.so" .
sha256sum "$project/dist/linux-arm64/privacy-redactor.so"
file "$project/dist/linux-arm64/privacy-redactor.so"
