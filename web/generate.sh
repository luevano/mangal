#!/usr/bin/env bash

set -exo pipefail

DIR="$(dirname -- "${BASH_SOURCE[0]}")"
DIR="$(realpath "$DIR")"
UI_DIR="$DIR/ui"

OPENAPI_SCHEMA="$DIR/openapi.yaml"

main() {
	pushd "$DIR"
	# one spec: the browse routes (tag web) in api, the API v1 (tag v1) in apiv1
	for pkg in api apiv1; do
		pushd "$pkg"
		go tool oapi-codegen --config server.cfg.yaml "$OPENAPI_SCHEMA"
		go tool oapi-codegen --config types.cfg.yaml "$OPENAPI_SCHEMA"
		popd
	done

	"$UI_DIR/node_modules/.bin/openapi-typescript" "$OPENAPI_SCHEMA" -o "$UI_DIR/src/api/mangal.ts"

	pushd ui
	npm run build
	popd

	popd
}

main
