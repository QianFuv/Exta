/* global localStorage, document, window, MutationObserver */
/** Synchronize Semi's resolved body tokens before the first content paint. */
(() => {
  let theme = "system";
  try {
    const saved = localStorage.getItem("exta.theme");
    if (saved === "light" || saved === "dark" || saved === "system") theme = saved;
  } catch {
    // Storage is optional; current-session appearance remains usable.
  }
  document.documentElement.dataset.theme = theme;
  const media = window.matchMedia?.("(prefers-color-scheme: dark)");
  const apply = () => {
    const preference = document.documentElement.dataset.theme;
    const resolved =
      preference === "dark" || (preference === "system" && media?.matches) ? "dark" : "light";
    document.body?.setAttribute("theme-mode", resolved);
  };
  new MutationObserver(apply).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-theme"],
    childList: true,
  });
  media?.addEventListener("change", apply);
  apply();
})();
