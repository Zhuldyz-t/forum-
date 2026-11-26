// Runs in <head> before the page paints, so the dark theme (the default)
// never flashes white. A separate file because the CSP forbids inline scripts.
(function () {
  var t = null;
  try { t = localStorage.getItem('theme'); } catch (e) {}
  if (t !== 'light' && t !== 'dark') t = 'dark';
  document.documentElement.setAttribute('data-theme', t);
})();
