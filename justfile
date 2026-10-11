#!/usr/bin/env just --justfile
set positional-arguments

go-mod := `go list`

default: run

# just run without compiling/installling
run *args='':
    go run -tags webui . $@

# install mangal to the ~/go/bin
install:
    go install -tags webui -ldflags "-s -w" .

# build
build:
    go build -tags webui -ldflags "-s -w" .

# run tests
test:
    go test ./...

# generate assets
generate:
    go generate ./...
    ./web/generate.sh

# update deps
update:
    go get -u
    go mod tidy -v

# generate and install mangal
full: update generate test install

# publish
publish tag:
    GOPROXY=proxy.golang.org go list -m {{go-mod}}@{{tag}}
