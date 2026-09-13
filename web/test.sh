#!/bin/sh
# test.sh — runs everything that verifies the WASM build, which a plain
# `go test ./...` cannot: cmd/wasm needs a js/wasm build target, so its tests
# are silently skipped by the normal suite. Run this alongside `go test ./...`
# whenever you touch cmd/wasm, internal/wasmapi, or anything in web/.
set -e
cd "$(dirname "$0")/.."

if ! command -v node >/dev/null 2>&1; then
	echo "test.sh: node is required for the smoke test (it loads main.wasm the way a browser would)" >&2
	exit 1
fi

if ! node -e '
const [major, minor] = process.versions.node.split(".").map(Number);
const ok = major > 22 || (major === 22 && minor >= 7) || (major === 20 && minor >= 19);
process.exit(ok ? 0 : 1);
'; then
	echo "test.sh: requires Node 20.19+ or 22.7+ (needed for unflagged ES module detection in extensionless .js files); found $(node --version)" >&2
	exit 1
fi

echo "==> Building the WASM module"
./web/build.sh

echo "==> Go tests for cmd/wasm (js/wasm target, run under Node via go_js_wasm_exec)"
GOOS=js GOARCH=wasm GOFLAGS="-exec=$(go env GOROOT)/lib/wasm/go_js_wasm_exec" go test ./cmd/wasm/...

echo "==> Node smoke test against the compiled module"
node web/smoketest.mjs

echo "==> Node syntax-highlighter tests"
node web/highlight_test.mjs

echo "==> Checking index.html/app.js element-ID contract"
node web/check-ids.mjs

echo "All WASM checks passed."
