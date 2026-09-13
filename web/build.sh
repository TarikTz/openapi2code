#!/bin/sh
set -e
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm go build -o web/main.wasm ./cmd/wasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/wasm_exec.js
echo "Built web/main.wasm and copied web/wasm_exec.js"

# style.css is committed (every page links it directly, with no CDN
# fallback), so this keeps it in sync with tailwind.css and each page's
# actual class usage whenever either changes — run this before deploying.
(cd web && npm install --no-audit --no-fund && npm run build:css)
echo "Built web/style.css"
