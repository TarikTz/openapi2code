// app.js — the OpenAPI2Code playground: loads the WASM module and wires
// up live (debounced) generation, the example gallery, tabbed output,
// syntax highlighting, theme toggling, and copy-to-clipboard.
import { EXAMPLES } from "./examples.js";
import { highlightCode, languageForFile } from "./highlight.js";

const DEBOUNCE_MS = 300;
const THEME_STORAGE_KEY = "openapi2code-theme";

// Lucide's createIcons() replaces each <i data-lucide="..."> with a new
// <svg>, carrying over id/class/other attributes. It must run before the
// getElementById calls below, or those calls capture the original <i>
// elements, which are then discarded from the DOM when createIcons runs.
// Guarded: if the Lucide CDN script failed to load (offline, CDN blocked),
// window.lucide is undefined — degrade to missing icons rather than
// throwing on the module's first statement and killing the whole page.
if (window.lucide) {
  window.lucide.createIcons();
}

const specInput = document.getElementById("spec-input");
const examplesButton = document.getElementById("examples-button");
const examplesMenu = document.getElementById("examples-menu");
const targetSelect = document.getElementById("target-select");
const modularToggle = document.getElementById("modular-toggle");
const urlInput = document.getElementById("url-input");
const urlFetchButton = document.getElementById("url-fetch-button");
const fileTabs = document.getElementById("file-tabs");
const outputCode = document.getElementById("output-code");
const copyButton = document.getElementById("copy-button");
const copyIcon = document.getElementById("copy-icon");
const copyIconSuccess = document.getElementById("copy-icon-success");
const themeToggle = document.getElementById("theme-toggle");
const themeIconSun = document.getElementById("theme-icon-sun");
const themeIconMoon = document.getElementById("theme-icon-moon");

let currentFiles = null; // the last successful generation's { "<name>": "<content>" }
let activeFile = null; // filename of the currently displayed tab
let debounceHandle = null;

// --- WASM loading (same pattern as sub-project 4's test page) ---

async function instantiate(importObject) {
  try {
    return await WebAssembly.instantiateStreaming(fetch("main.wasm"), importObject);
  } catch (err) {
    // Re-fetch rather than reusing the first response: instantiateStreaming
    // may already have consumed its body, which would make arrayBuffer()
    // throw "body already read" and mask the real problem.
    const bytes = await (await fetch("main.wasm")).arrayBuffer();
    return await WebAssembly.instantiate(bytes, importObject);
  }
}

async function loadWasm() {
  const go = new window.Go();
  const result = await instantiate(go.importObject);
  go.run(result.instance); // never resolves — main() runs select{} forever; don't await it
  while (typeof window.openapi2codeGenerate !== "function") {
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

function setControlsEnabled(enabled) {
  specInput.disabled = !enabled;
  targetSelect.disabled = !enabled;
  modularToggle.disabled = !enabled;
  urlInput.disabled = !enabled;
  urlFetchButton.disabled = !enabled;
  examplesButton.disabled = !enabled;
}

// --- Example gallery ---

function closeExamplesMenu() {
  examplesMenu.hidden = true;
  examplesButton.setAttribute("aria-expanded", "false");
}

function openExamplesMenu() {
  examplesMenu.hidden = false;
  examplesButton.setAttribute("aria-expanded", "true");
}

function renderExampleButtons() {
  for (const example of EXAMPLES) {
    const item = document.createElement("button");
    item.type = "button";
    item.setAttribute("role", "menuitem");
    item.className =
      "flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left hover:bg-ink/5 dark:hover:bg-paper/10 focus-visible:outline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-brace dark:focus-visible:outline-brace-light";
    item.innerHTML = `
      <span class="font-mono text-sm">${example.label}</span>
      <span class="text-xs text-ink/60 dark:text-paper/60">${example.description}</span>
    `;
    item.addEventListener("click", () => {
      specInput.value = example.spec;
      generate();
      closeExamplesMenu();
      examplesButton.focus();
    });
    examplesMenu.appendChild(item);
  }
}

examplesButton.addEventListener("click", () => {
  if (examplesMenu.hidden) {
    openExamplesMenu();
  } else {
    closeExamplesMenu();
  }
});

document.addEventListener("click", (event) => {
  if (!examplesMenu.hidden && !examplesMenu.contains(event.target) && event.target !== examplesButton) {
    closeExamplesMenu();
  }
});

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && !examplesMenu.hidden) {
    closeExamplesMenu();
    examplesButton.focus();
  }
});

// --- Generation ---

function showError(message) {
  currentFiles = null;
  activeFile = null;
  fileTabs.innerHTML = "";
  outputCode.className = "font-mono text-red-600 dark:text-red-400";
  outputCode.textContent = "Error: " + message;
}

function showPlaceholder(message) {
  currentFiles = null;
  activeFile = null;
  fileTabs.innerHTML = "";
  outputCode.className = "font-mono text-ink/50 dark:text-paper/50";
  outputCode.textContent = message;
}

// --- Loading a spec from a URL or a dropped file ---

async function fetchSpecFromURL() {
  const url = urlInput.value.trim();
  if (url === "") {
    return;
  }
  try {
    const response = await fetch(url);
    if (!response.ok) {
      showError(`fetch ${url}: unexpected status ${response.status} ${response.statusText}`);
      return;
    }
    specInput.value = await response.text();
    generate();
  } catch (err) {
    // A browser fetch() rejects with a generic, deliberately
    // uninformative TypeError for most failure causes (DNS, network, and
    // — overwhelmingly the actual reason in practice — the target server
    // not sending an Access-Control-Allow-Origin header) without
    // exposing which one, for cross-origin information-leak reasons the
    // browser itself enforces. Naming the likely cause explicitly here
    // is more honest than repeating the browser's own opaque message.
    showError(
      `couldn't fetch ${url} — this is almost always the server not allowing cross-origin requests (CORS), which no setting on this page can work around. Paste the spec's contents directly instead.`
    );
  }
}

function readDroppedFile(file) {
  const reader = new FileReader();
  reader.onload = () => {
    specInput.value = typeof reader.result === "string" ? reader.result : "";
    generate();
  };
  reader.onerror = () => {
    showError(`couldn't read ${file.name}: ${reader.error?.message ?? "unknown error"}`);
  };
  reader.readAsText(file);
}

function setDropActive(active) {
  specInput.classList.toggle("outline", active);
  specInput.classList.toggle("outline-2", active);
  specInput.classList.toggle("outline-brace", active);
  specInput.classList.toggle("dark:outline-brace-light", active);
}

function renderActiveFile() {
  outputCode.className = "font-mono";
  outputCode.innerHTML = highlightCode(currentFiles[activeFile], languageForFile(activeFile));
}

function renderFileTabs() {
  fileTabs.innerHTML = "";
  const names = Object.keys(currentFiles);
  if (!names.includes(activeFile)) {
    activeFile = names[0];
  }
  for (const name of names) {
    const tab = document.createElement("button");
    tab.type = "button";
    tab.textContent = name;
    const isActive = name === activeFile;
    tab.className = isActive
      ? "rounded-sm px-3 py-1 text-sm font-mono bg-brace dark:bg-brace-light text-white dark:text-ink focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brace dark:focus-visible:outline-brace-light"
      : "rounded-sm px-3 py-1 text-sm font-mono border border-ink/15 dark:border-paper/15 hover:bg-ink/5 dark:hover:bg-paper/10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brace dark:focus-visible:outline-brace-light";
    tab.addEventListener("click", () => {
      activeFile = name;
      renderFileTabs();
      renderActiveFile();
    });
    fileTabs.appendChild(tab);
  }
}

function generate() {
  const specText = specInput.value;
  if (specText.trim() === "") {
    showPlaceholder("Paste a spec or pick an example above to get started.");
    return;
  }
  const modular = modularToggle.getAttribute("aria-pressed") === "true";
  let raw;
  try {
    raw = window.openapi2codeGenerate(specText, targetSelect.value, modular);
  } catch (err) {
    // A deeply nested spec that somehow still slips past internal/ir's
    // depth guard (or any other unexpected WASM-side panic) surfaces as
    // a thrown JS exception rather than a {"error": ...} result.
    showError(err && err.message ? err.message : String(err));
    return;
  }
  const result = JSON.parse(raw);
  if (result.error) {
    showError(result.error);
    return;
  }
  currentFiles = result.files;
  renderFileTabs();
  renderActiveFile();
}

function scheduleGenerate() {
  if (debounceHandle !== null) {
    clearTimeout(debounceHandle);
  }
  debounceHandle = setTimeout(() => {
    debounceHandle = null;
    generate();
  }, DEBOUNCE_MS);
}

// --- Modular toggle ---

function setModular(active) {
  modularToggle.setAttribute("aria-pressed", String(active));
  modularToggle.classList.toggle("bg-brace", active);
  modularToggle.classList.toggle("dark:bg-brace-light", active);
  modularToggle.classList.toggle("text-white", active);
  modularToggle.classList.toggle("dark:text-ink", active);
  modularToggle.classList.toggle("border-brace", active);
  modularToggle.classList.toggle("dark:border-brace-light", active);
  scheduleGenerate();
}

modularToggle.addEventListener("click", () => {
  setModular(modularToggle.getAttribute("aria-pressed") !== "true");
});

// --- Copy to clipboard ---

copyButton.addEventListener("click", async () => {
  if (currentFiles === null || activeFile === null) {
    return;
  }
  try {
    await navigator.clipboard.writeText(currentFiles[activeFile]);
  } catch (err) {
    // navigator.clipboard is undefined (or writeText rejects) in any
    // non-secure context — file://, or plain HTTP on a LAN IP instead of
    // localhost. Surface a brief inline failure state instead of letting
    // this reject silently with no user feedback.
    copyButton.classList.add("text-red-600", "dark:text-red-400");
    copyButton.setAttribute("aria-label", "Copy failed");
    setTimeout(() => {
      copyButton.classList.remove("text-red-600", "dark:text-red-400");
      copyButton.setAttribute("aria-label", "Copy to clipboard");
    }, 1200);
    return;
  }
  copyIcon.classList.toggle("hidden", true);
  copyIconSuccess.classList.toggle("hidden", false);
  setTimeout(() => {
    copyIcon.classList.toggle("hidden", false);
    copyIconSuccess.classList.toggle("hidden", true);
  }, 1200);
});

// --- Theme ---

function applyTheme(isDark) {
  document.documentElement.classList.toggle("dark", isDark);
  themeIconSun.classList.toggle("hidden", isDark);
  themeIconMoon.classList.toggle("hidden", !isDark);
}

function initTheme() {
  // localStorage can throw (Safari file://, blocked site data). initTheme
  // runs at module top level before loadWasm() is kicked off, so letting
  // that exception escape would abort the rest of the module and strand
  // the page on "Loading WASM module..." forever with every control
  // disabled. Fall back to the system preference instead.
  let stored = null;
  try {
    stored = localStorage.getItem(THEME_STORAGE_KEY);
  } catch (err) {
    // Ignored — fall back to system preference below.
  }
  const isDark = stored ? stored === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches;
  applyTheme(isDark);
}

themeToggle.addEventListener("click", () => {
  const isDark = !document.documentElement.classList.contains("dark");
  try {
    localStorage.setItem(THEME_STORAGE_KEY, isDark ? "dark" : "light");
  } catch (err) {
    // Ignored — the theme still applies for this session, it just won't
    // persist across a reload.
  }
  applyTheme(isDark);
});

// --- Wiring ---

specInput.addEventListener("input", scheduleGenerate);
targetSelect.addEventListener("change", scheduleGenerate);

urlFetchButton.addEventListener("click", fetchSpecFromURL);
urlInput.addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    event.preventDefault();
    fetchSpecFromURL();
  }
});

// dragover must call preventDefault, or the browser's default handling
// (typically navigating to/opening the dropped file) fires instead of
// the drop event below.
specInput.addEventListener("dragover", (event) => {
  event.preventDefault();
  setDropActive(true);
});
specInput.addEventListener("dragleave", () => setDropActive(false));
specInput.addEventListener("drop", (event) => {
  event.preventDefault();
  setDropActive(false);
  if (specInput.disabled) {
    return; // still loading the WASM module
  }
  const file = event.dataTransfer?.files?.[0];
  if (file) {
    readDroppedFile(file);
  }
});

renderExampleButtons();
initTheme();
showPlaceholder("Loading WASM module...");

loadWasm()
  .then(() => {
    setControlsEnabled(true);
    showPlaceholder("Paste a spec or pick an example above to get started.");
  })
  .catch((err) => {
    showError("Failed to load WASM module: " + (err && err.message ? err.message : String(err)));
  });
