// Applies the saved theme (Settings → Profile) before first paint; no choice follows the system.
try {
  var t = localStorage.getItem('taskwell:theme')
  if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t
} catch (e) {}
