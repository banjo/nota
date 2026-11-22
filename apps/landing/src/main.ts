import "./styles.css";

// The landing page's only behaviour: a light/dark switch that remembers
// the choice, and the current year in the footer's copyright line.

const root = document.documentElement;
const button = document.getElementById("theme");

function current(): "light" | "dark" {
  const set = root.dataset.theme;
  if (set === "light" || set === "dark") return set;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function apply(theme: "light" | "dark") {
  root.dataset.theme = theme;
  try {
    localStorage.setItem("nota:theme", theme);
  } catch {
    // Private browsing: the choice lasts this page.
  }
  button?.setAttribute("aria-label", `Switch to ${theme === "dark" ? "light" : "dark"} mode`);
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", theme === "dark" ? "#1c1c1e" : "#f7f7f5");
}

button?.addEventListener("click", () => apply(current() === "dark" ? "light" : "dark"));
button?.setAttribute("aria-label", `Switch to ${current() === "dark" ? "light" : "dark"} mode`);

// The page follows the system until the visitor picks one.
window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", (e) => {
  if (!root.dataset.theme)
    button?.setAttribute("aria-label", `Switch to ${e.matches ? "light" : "dark"} mode`);
});
