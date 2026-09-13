// check-ids.mjs — verifies every id web/app.js looks up via
// getElementById actually exists in web/index.html. This is the one
// contract nothing else in this test suite protects: smoketest.mjs
// drives the WASM module directly and never loads index.html or app.js,
// so a rename on either side would otherwise go undetected until someone
// opens the page in a browser.
import { readFile } from "node:fs/promises";

const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
const app = await readFile(new URL("./app.js", import.meta.url), "utf8");

const htmlIds = new Set([...html.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1]));
const lookedUp = [...app.matchAll(/getElementById\("([^"]+)"\)/g)].map((m) => m[1]);

const missing = lookedUp.filter((id) => !htmlIds.has(id));
if (missing.length > 0) {
  console.error(`FAIL: app.js looks up ids not present in index.html: ${missing.join(", ")}`);
  process.exit(1);
}
console.log(`OK: all ${lookedUp.length} getElementById lookups in app.js resolve to ids in index.html`);
process.exit(0);
