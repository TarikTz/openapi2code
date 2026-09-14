// check-ids.mjs — verifies every id web/app.js looks up via
// getElementById actually exists in every HTML page that loads app.js
// (index.html and playground.html share the one script). This is the
// one contract nothing else in this test suite protects: smoketest.mjs
// drives the WASM module directly and never loads either page or app.js,
// so a rename on either side would otherwise go undetected until someone
// opens a page in a browser.
import { readFile } from "node:fs/promises";

const PAGES = ["index.html", "playground.html"];
const app = await readFile(new URL("./app.js", import.meta.url), "utf8");
const lookedUp = [...app.matchAll(/getElementById\("([^"]+)"\)/g)].map((m) => m[1]);

let failed = false;
for (const page of PAGES) {
  const html = await readFile(new URL(`./${page}`, import.meta.url), "utf8");
  const htmlIds = new Set([...html.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1]));
  const missing = lookedUp.filter((id) => !htmlIds.has(id));
  if (missing.length > 0) {
    console.error(`FAIL: app.js looks up ids not present in ${page}: ${missing.join(", ")}`);
    failed = true;
  } else {
    console.log(`OK: all ${lookedUp.length} getElementById lookups in app.js resolve to ids in ${page}`);
  }
}
process.exit(failed ? 1 : 0);
