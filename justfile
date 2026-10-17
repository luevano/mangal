#!/usr/bin/env just --justfile
set positional-arguments

go-mod := `go list`

default: run

# just run without compiling/installling
run *args='': ui-dist
    go run -tags webui . $@

# build the web UI and install mangal to $GOBIN (~/go/bin)
install: ui
    go install -tags webui -trimpath -ldflags "-s -w" .

# build the web UI and mangal
build: ui
    go build -tags webui -trimpath -ldflags "-s -w" .

# build the web UI (web/ui/dist, embedded by the webui tag), needs node ^22.18 or >=24.12
ui:
    @command -v npm >/dev/null || { echo "npm not found: the web UI needs node ^22.18 or >=24.12"; exit 1; }
    cd web/ui && npm ci && npm run build-only

# build the web UI only when it isn't built yet
[private]
ui-dist:
    @test -f web/ui/dist/index.html || just ui

# run tests
test:
    go test -race ./...

# generate assets
generate: ui-dist
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
