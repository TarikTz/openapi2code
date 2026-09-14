// check-ids.mjs — verifies every id web/app.js looks up via
// getElementById exists on at least one HTML page that loads app.js
// (index.html and playground.html share the one script, but some ids —
// e.g. the saved-specs sidebar — are only present on one of them; app.js
// guards those lookups with a presence check before wiring anything up).
// This is the one contract nothing else in this test suite protects:
// smoketest.mjs drives the WASM module directly and never loads either
// page or app.js, so a rename on either side would otherwise go
// undetected until someone opens a page in a browser.
import { readFile } from "node:fs/promises";

const PAGES = ["index.html", "playground.html"];
const app = await readFile(new URL("./app.js", import.meta.url), "utf8");
const lookedUp = [...app.matchAll(/getElementById\("([^"]+)"\)/g)].map((m) => m[1]);

const idsByPage = new Map();
for (const page of PAGES) {
  const html = await readFile(new URL(`./${page}`, import.meta.url), "utf8");
  idsByPage.set(page, new Set([...html.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1])));
}

const missingEverywhere = lookedUp.filter((id) => !PAGES.some((page) => idsByPage.get(page).has(id)));
if (missingEverywhere.length > 0) {
  console.error(`FAIL: app.js looks up ids not present on any page: ${missingEverywhere.join(", ")}`);
  process.exit(1);
}
console.log(`OK: all ${lookedUp.length} getElementById lookups in app.js resolve to an id on at least one of ${PAGES.join(", ")}`);
process.exit(0);
