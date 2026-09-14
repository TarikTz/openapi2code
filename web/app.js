// app.js — the OpenAPI2Code playground: loads the WASM module and wires
// up live (debounced) generation, the example gallery, tabbed output,
// syntax highlighting, theme toggling, copy-to-clipboard, and (on
// playground.html only) the saved-specs sidebar.
import { EXAMPLES } from "./examples.js";
import { highlightCode, languageForFile } from "./highlight.js";

const DEBOUNCE_MS = 300;
const THEME_STORAGE_KEY = "openapi2code-theme";
const SAVED_SPECS_KEY = "openapi2code-saved-specs";

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

// Only playground.html has a sidebar; the homepage's embedded playground
// doesn't, so these are all null there and every block below is guarded
// on sidebarSaveButton existing rather than forking into two scripts.
const sidebarSaveButton = document.getElementById("sidebar-save-button");
const sidebarList = document.getElementById("sidebar-list");
const sidebarMessage = document.getElementById("sidebar-message");
const sidebarToggle = document.getElementById("sidebar-toggle");
const sidebarPanel = document.getElementById("sidebar-panel");

let currentFiles = null; // the last successful generation's { "<name>": "<content>" }
let activeFile = null; // filename of the currently displayed tab
let debounceHandle = null;

// --- WASM loading (same pattern as sub-project 4's test page) ---

async function instantiate(importObject) {
  // priority: "low" deprioritizes this ~1MB+ fetch behind render-blocking
  // CSS/fonts/icons — those finish first so the page still paints promptly
  // even though this kicks off immediately. Chrome-only; ignored elsewhere.
  try {
    return await WebAssembly.instantiateStreaming(fetch("main.wasm", { priority: "low" }), importObject);
  } catch (err) {
    // Re-fetch rather than reusing the first response: instantiateStreaming
    // may already have consumed its body, which would make arrayBuffer()
    // throw "body already read" and mask the real problem.
    const bytes = await (await fetch("main.wasm", { priority: "low" })).arrayBuffer();
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
  if (sidebarSaveButton) {
    sidebarSaveButton.disabled = !enabled;
  }
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

// --- Sidebar: named, explicitly-saved specs (playground.html only, see
// the sidebar* element lookups above) ---

function loadSavedSpecs() {
  try {
    const parsed = JSON.parse(localStorage.getItem(SAVED_SPECS_KEY) ?? "[]");
    return Array.isArray(parsed) ? parsed : [];
  } catch (err) {
    return [];
  }
}

function writeSavedSpecs(specs) {
  try {
    localStorage.setItem(SAVED_SPECS_KEY, JSON.stringify(specs));
    return true;
  } catch (err) {
    // Quota exceeded, or blocked site data — surfaced to the user by the
    // caller rather than failing silently, since a save that didn't
    // actually happen is exactly the kind of thing this feature exists
    // to prevent.
    return false;
  }
}

function showSidebarMessage(text) {
  sidebarMessage.textContent = text;
  setTimeout(() => {
    if (sidebarMessage.textContent === text) {
      sidebarMessage.textContent = "";
    }
  }, 2000);
}

// Best-effort label pulled from the spec's own info.title/info.version,
// whether the spec is JSON or YAML — this is cosmetic sidebar labeling
// only, never treated as validated OpenAPI, so a plain regex scan for the
// YAML case is enough; a timestamp is the fallback when neither is found.
function guessSpecName(specText) {
  try {
    const parsed = JSON.parse(specText);
    const title = parsed?.info?.title;
    if (typeof title === "string" && title.trim() !== "") {
      const version = parsed.info.version;
      return typeof version === "string" && version.trim() !== "" ? `${title} v${version}` : title;
    }
  } catch (err) {
    // Not JSON — fall through to the YAML-ish scan below.
  }
  const titleMatch = specText.match(/^\s*title:\s*["']?(.+?)["']?\s*$/m);
  if (titleMatch) {
    const versionMatch = specText.match(/^\s*version:\s*["']?(.+?)["']?\s*$/m);
    return versionMatch ? `${titleMatch[1]} v${versionMatch[1]}` : titleMatch[1];
  }
  return `Saved ${new Date().toLocaleString()}`;
}

function renderSidebarList() {
  const specs = loadSavedSpecs();
  sidebarList.innerHTML = "";
  if (specs.length === 0) {
    const empty = document.createElement("p");
    empty.className = "text-xs text-ink/50 dark:text-paper/50 px-2";
    empty.textContent = "Nothing saved yet.";
    sidebarList.appendChild(empty);
    return;
  }
  for (const entry of specs) {
    const row = document.createElement("div");
    row.className = "group flex items-start gap-1 rounded-sm px-2 py-1.5 hover:bg-ink/5 dark:hover:bg-paper/10";

    const loadButton = document.createElement("button");
    loadButton.type = "button";
    loadButton.className = "flex-1 min-w-0 text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brace dark:focus-visible:outline-brace-light";
    const nameSpan = document.createElement("span");
    nameSpan.className = "block truncate text-sm font-mono";
    nameSpan.textContent = entry.name;
    const dateSpan = document.createElement("span");
    dateSpan.className = "block text-xs text-ink/50 dark:text-paper/50";
    dateSpan.textContent = new Date(entry.savedAt).toLocaleString();
    loadButton.appendChild(nameSpan);
    loadButton.appendChild(dateSpan);
    loadButton.addEventListener("click", () => {
      if (specInput.disabled) {
        return; // still loading the WASM module
      }
      specInput.value = entry.spec;
      generate();
    });

    const renameButton = document.createElement("button");
    renameButton.type = "button";
    renameButton.setAttribute("aria-label", `Rename "${entry.name}"`);
    renameButton.className =
      "shrink-0 p-1 rounded-sm opacity-0 group-hover:opacity-100 focus-visible:opacity-100 hover:bg-ink/10 dark:hover:bg-paper/20 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brace dark:focus-visible:outline-brace-light";
    renameButton.innerHTML =
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-3.5 h-3.5" aria-hidden="true"><path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/><path d="m15 5 4 4"/></svg>';
    renameButton.addEventListener("click", () => {
      const nextName = prompt("Rename saved spec", entry.name);
      if (nextName === null) {
        return;
      }
      const trimmed = nextName.trim();
      if (trimmed === "") {
        return;
      }
      const specs2 = loadSavedSpecs();
      const target = specs2.find((s) => s.id === entry.id);
      if (target) {
        target.name = trimmed;
        writeSavedSpecs(specs2);
        renderSidebarList();
      }
    });

    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    deleteButton.setAttribute("aria-label", `Delete "${entry.name}"`);
    deleteButton.className =
      "shrink-0 p-1 rounded-sm opacity-0 group-hover:opacity-100 focus-visible:opacity-100 hover:bg-ink/10 dark:hover:bg-paper/20 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brace dark:focus-visible:outline-brace-light";
    deleteButton.innerHTML =
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-3.5 h-3.5" aria-hidden="true"><path d="M10 11v6"/><path d="M14 11v6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M3 6h18"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>';
    deleteButton.addEventListener("click", () => {
      if (!confirm(`Delete "${entry.name}"?`)) {
        return;
      }
      writeSavedSpecs(loadSavedSpecs().filter((s) => s.id !== entry.id));
      renderSidebarList();
    });

    row.appendChild(loadButton);
    row.appendChild(renameButton);
    row.appendChild(deleteButton);
    sidebarList.appendChild(row);
  }
}

if (sidebarSaveButton) {
  sidebarSaveButton.addEventListener("click", () => {
    const specText = specInput.value;
    if (specText.trim() === "") {
      showSidebarMessage("Nothing to save.");
      return;
    }
    const entry = {
      id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      name: guessSpecName(specText),
      spec: specText,
      savedAt: new Date().toISOString(),
    };
    const specs = loadSavedSpecs();
    specs.unshift(entry);
    if (writeSavedSpecs(specs)) {
      renderSidebarList();
      showSidebarMessage("Saved.");
    } else {
      showSidebarMessage("Couldn't save — storage might be full.");
    }
  });
  renderSidebarList();
}

if (sidebarToggle) {
  sidebarToggle.addEventListener("click", () => {
    // Swap "hidden" for a plain "flex" (rather than just removing
    // "hidden") so the panel actually gets display:flex below the md
    // breakpoint — "md:flex" alone has no effect until then. Above md the
    // panel stays visible either way, since sidebar-toggle itself is
    // md:hidden and never fires there.
    const opening = sidebarPanel.classList.contains("hidden");
    sidebarPanel.classList.toggle("hidden", !opening);
    sidebarPanel.classList.toggle("flex", opening);
  });
}

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

// Deferred to idle time (falling back to a macrotask where
// requestIdleCallback doesn't exist, e.g. Safari) so fetching and
// compiling the WASM module doesn't compete with the initial paint for
// the main thread — the page still becomes interactive as soon as it's
// ready, just without holding up first paint to get there.
(window.requestIdleCallback || ((cb) => setTimeout(cb, 0)))(() => {
  loadWasm()
    .then(() => {
      setControlsEnabled(true);
      showPlaceholder("Paste a spec or pick an example above to get started.");
    })
    .catch((err) => {
      showError("Failed to load WASM module: " + (err && err.message ? err.message : String(err)));
    });
});
