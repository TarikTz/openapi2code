// smoketest.mjs — verifies the compiled WASM module works when loaded
// the same way the browser test page (app.js) loads it, without needing
// a real browser. Requires no npm packages (Node's built-in fetch/
// WebAssembly are sufficient). Run after web/build.sh:
//   node web/smoketest.mjs
import { readFile } from "node:fs/promises";
import "./wasm_exec.js";

const go = new globalThis.Go();
const wasmBuffer = await readFile(new URL("./main.wasm", import.meta.url));
const { instance } = await WebAssembly.instantiate(wasmBuffer, go.importObject);
go.run(instance); // fire and forget — main() never returns (select{})

while (typeof globalThis.openapi2codeGenerate !== "function") {
  await new Promise((resolve) => setTimeout(resolve, 10));
}

const spec = JSON.stringify({
  openapi: "3.0.3",
  components: {
    schemas: {
      Pet: {
        type: "object",
        properties: { name: { type: "string" } },
        required: ["name"],
      },
    },
  },
});

function check(label, raw, wantSubstring) {
  const result = JSON.parse(raw);
  if (result.error) {
    console.error(`FAIL (${label}): unexpected error: ${result.error}`);
    process.exit(1);
  }
  // Search every file's content rather than a hardcoded key: monolithic and
  // modular layouts use different file names per target (index.ts/Pet.ts,
  // Generated.swift/Pet.swift, Generated.kt/Pet.kt, generated.dart/pet.dart),
  // and this check only cares that the expected declaration shows up
  // somewhere, not which file it landed in.
  const content = Object.values(result.files).join("\n");
  if (!content.includes(wantSubstring)) {
    console.error(`FAIL (${label}): output missing ${JSON.stringify(wantSubstring)}:`, result.files);
    process.exit(1);
  }
  console.log(`OK (${label}):`, Object.keys(result.files));
}

check("ts monolithic", globalThis.openapi2codeGenerate(spec, "ts", false), "export interface Pet {");
check("zod modular", globalThis.openapi2codeGenerate(spec, "zod", true), "export const PetSchema = z.object({");
check("swift monolithic", globalThis.openapi2codeGenerate(spec, "swift", false), "public struct Pet: Codable {");
check("kotlin modular", globalThis.openapi2codeGenerate(spec, "kotlin", true), "data class Pet(");
check("dart modular", globalThis.openapi2codeGenerate(spec, "dart", true), "class Pet {");

const errRaw = globalThis.openapi2codeGenerate("", "ts", false);
const errResult = JSON.parse(errRaw);
if (!errResult.error) {
  console.error("FAIL (error path): expected an error for an empty spec, got:", errResult);
  process.exit(1);
}
console.log("OK (error path):", errResult.error);

console.log("All smoketest checks passed.");
process.exit(0);
