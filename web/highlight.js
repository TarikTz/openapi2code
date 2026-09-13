// highlight.js — a minimal regex-based syntax highlighter for the
// languages this project generates. Not a general-purpose highlighter
// for any of them: it only needs to handle what internal/gen/{ts,zod,
// swift,kotlin,dart} actually emit (declarations, simple control flow,
// string/enum literals, comments, numbers) — each language's keyword
// list below was pulled directly from what its generator's templates
// use, not from the full language grammar.

const KEYWORDS_BY_LANG = {
  ts: new Set([
    "export", "import", "interface", "type", "const", "let", "function",
    "return", "from", "extends", "readonly", "typeof", "as", "null",
    "undefined", "true", "false", "void", "namespace",
  ]),
  swift: new Set([
    "as", "case", "catch", "class", "default", "else", "enum", "extension",
    "false", "for", "func", "guard", "if", "import", "in", "init",
    "internal", "is", "let", "nil", "private", "protocol", "public",
    "return", "self", "Self", "static", "struct", "switch", "throw",
    "throws", "true", "try", "typealias", "var", "where",
  ]),
  kotlin: new Set([
    "as", "class", "companion", "data", "else", "enum", "false", "for",
    "fun", "if", "import", "in", "internal", "is", "null", "object",
    "return", "true", "typealias", "val", "var", "when",
  ]),
  dart: new Set([
    "as", "class", "const", "dynamic", "else", "enum", "factory", "false",
    "final", "for", "if", "import", "in", "is", "null", "required",
    "return", "static", "this", "true", "var", "void",
  ]),
};

// languageForFile maps a generated file's name to a highlighter language
// by its extension — the extension is what the engine's output actually
// carries, so it's a more reliable signal than the target picker's value
// (e.g. both "ts" and "zod" targets emit .ts files, and should highlight
// identically).
export function languageForFile(filename) {
  if (filename.endsWith(".swift")) return "swift";
  if (filename.endsWith(".kt")) return "kotlin";
  if (filename.endsWith(".dart")) return "dart";
  return "ts";
}

// Alternation order matters: comments and strings must be matched whole
// before their contents could be mistaken for other tokens (e.g. a
// keyword-like word inside a string).
const TOKEN_RE =
  /(\/\/[^\n]*)|("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`)|(\b\d+(?:\.\d+)?\b)|([A-Za-z_$][A-Za-z0-9_$]*)|([{}()[\]:;,.<>|&=?!@])/g;

function escapeHtml(text) {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

// highlightCode wraps recognized tokens in `code` with <span class="tok-*">
// markers and HTML-escapes everything else (including token contents), so
// the result is always safe to assign to an element's innerHTML. lang
// selects the keyword table (see KEYWORDS_BY_LANG); an unrecognized lang
// falls back to the ts table.
export function highlightCode(code, lang) {
  const keywords = KEYWORDS_BY_LANG[lang] || KEYWORDS_BY_LANG.ts;
  let result = "";
  let lastIndex = 0;
  let match;
  TOKEN_RE.lastIndex = 0;
  while ((match = TOKEN_RE.exec(code)) !== null) {
    result += escapeHtml(code.slice(lastIndex, match.index));
    const [, comment, string, number, word, punct] = match;
    if (comment !== undefined) {
      result += `<span class="tok-comment">${escapeHtml(comment)}</span>`;
    } else if (string !== undefined) {
      result += `<span class="tok-string">${escapeHtml(string)}</span>`;
    } else if (number !== undefined) {
      result += `<span class="tok-number">${escapeHtml(number)}</span>`;
    } else if (word !== undefined) {
      if (keywords.has(word)) {
        result += `<span class="tok-keyword">${escapeHtml(word)}</span>`;
      } else if (/^[A-Z]/.test(word)) {
        result += `<span class="tok-type">${escapeHtml(word)}</span>`;
      } else {
        result += escapeHtml(word);
      }
    } else if (punct !== undefined) {
      result += escapeHtml(punct);
    }
    lastIndex = TOKEN_RE.lastIndex;
  }
  result += escapeHtml(code.slice(lastIndex));
  return result;
}
