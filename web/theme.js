// theme.js — dark/light theme toggle, shared by every content page
// (use-cases, examples, privacy, terms, contact) except the playground
// itself (index.html), which keeps this same logic inline in app.js so
// its one critical page has no extra network request on the way to
// becoming interactive. Kept in sync by hand — this is a small, stable
// amount of logic, not worth a build step to share formally.
const THEME_STORAGE_KEY = "openapi2code-theme";

const themeToggle = document.getElementById("theme-toggle");
const themeIconSun = document.getElementById("theme-icon-sun");
const themeIconMoon = document.getElementById("theme-icon-moon");

function applyTheme(isDark) {
  document.documentElement.classList.toggle("dark", isDark);
  themeIconSun.classList.toggle("hidden", isDark);
  themeIconMoon.classList.toggle("hidden", !isDark);
}

function initTheme() {
  // localStorage can throw (Safari file://, blocked site data) — fall
  // back to the system preference rather than letting that exception
  // strand the page with no theme applied at all.
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

initTheme();
