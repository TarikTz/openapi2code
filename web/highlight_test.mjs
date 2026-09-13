// highlight_test.mjs — unit tests for the playground's syntax
// highlighter. No test framework: plain assertions, exit 1 on failure,
// matching the style of web/smoketest.mjs. Run with: node web/highlight_test.mjs
import { highlightCode, languageForFile } from "./highlight.js";

function assertContains(label, haystack, needle) {
  if (!haystack.includes(needle)) {
    console.error(`FAIL (${label}): expected output to contain ${JSON.stringify(needle)}, got:`, haystack);
    process.exit(1);
  }
  console.log(`OK (${label})`);
}

function assertNotContains(label, haystack, needle) {
  if (haystack.includes(needle)) {
    console.error(`FAIL (${label}): expected output NOT to contain ${JSON.stringify(needle)}, got:`, haystack);
    process.exit(1);
  }
  console.log(`OK (${label})`);
}

function assertEqual(label, actual, expected) {
  if (actual !== expected) {
    console.error(`FAIL (${label}): expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
    process.exit(1);
  }
  console.log(`OK (${label})`);
}

// --- ts (also covers zod output, which shares the ts file extension) ---

assertContains(
  "ts keyword",
  highlightCode("export interface Pet {", "ts"),
  '<span class="tok-keyword">export</span>'
);
assertContains(
  "ts type name",
  highlightCode("export interface Pet {", "ts"),
  '<span class="tok-type">Pet</span>'
);
assertContains(
  "ts string literal",
  highlightCode('const s = "hello";', "ts"),
  '<span class="tok-string">"hello"</span>'
);
assertContains(
  "ts number literal",
  highlightCode("const x = 42;", "ts"),
  '<span class="tok-number">42</span>'
);
assertContains(
  "ts line comment",
  highlightCode("// a comment\nconst x = 1;", "ts"),
  '<span class="tok-comment">// a comment</span>'
);
assertContains(
  "zod call still renders plain identifiers",
  highlightCode("z.object({ name: z.string() })", "ts"),
  "z.object"
);

// --- swift ---

assertContains(
  "swift keyword",
  highlightCode("public struct Pet: Codable {", "swift"),
  '<span class="tok-keyword">struct</span>'
);
assertContains(
  "swift type name",
  highlightCode("public struct Pet: Codable {", "swift"),
  '<span class="tok-type">Pet</span>'
);
assertContains(
  "swift optional punctuation preserved",
  highlightCode("public var id: Int?", "swift"),
  '<span class="tok-type">Int</span>?'
);

// --- kotlin ---

assertContains(
  "kotlin keyword",
  highlightCode("data class Pet(", "kotlin"),
  '<span class="tok-keyword">data</span>'
);
assertContains(
  "kotlin type name",
  highlightCode("data class Pet(", "kotlin"),
  '<span class="tok-type">Pet</span>'
);

// --- dart ---

assertContains(
  "dart keyword",
  highlightCode("factory Pet.fromJson(Map<String, dynamic> json) {", "dart"),
  '<span class="tok-keyword">factory</span>'
);
assertContains(
  "dart type name",
  highlightCode("factory Pet.fromJson(Map<String, dynamic> json) {", "dart"),
  '<span class="tok-type">Pet</span>'
);

// --- unrecognized language falls back to ts ---

assertContains(
  "unrecognized lang falls back to ts keywords",
  highlightCode("export interface Pet {", "unknown"),
  '<span class="tok-keyword">export</span>'
);

// --- languageForFile ---

assertEqual("languageForFile .swift", languageForFile("Pet.swift"), "swift");
assertEqual("languageForFile .kt", languageForFile("Pet.kt"), "kotlin");
assertEqual("languageForFile .dart", languageForFile("pet.dart"), "dart");
assertEqual("languageForFile .ts", languageForFile("index.ts"), "ts");
assertEqual("languageForFile unknown extension falls back to ts", languageForFile("Generated"), "ts");

// The highlighter's output is assigned to innerHTML by the playground, so
// any "<" or ">" in the source text — plausible in a string literal or
// comment inside generated code — must never reach the DOM unescaped.
const escaped = highlightCode('const s = "<script>alert(1)</script>";', "ts");
assertNotContains("HTML-unsafe input is escaped", escaped, "<script>alert(1)</script>");
assertContains("HTML-unsafe input is escaped (check)", escaped, "&lt;script&gt;");

assertContains("empty input", highlightCode("", "ts"), "");

console.log("All highlight tests passed.");
process.exit(0);
