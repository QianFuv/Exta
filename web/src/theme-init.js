/* global localStorage, document */
/** Apply the saved appearance before CSS and React can paint the application. */
(() => {
  let theme = "system";
  try {
    const saved = localStorage.getItem("exta.theme");
    if (saved === "light" || saved === "dark" || saved === "system") theme = saved;
  } catch {
    // Storage is optional; system appearance still works in restricted browsers.
  }
  document.documentElement.dataset.theme = theme;
})();
