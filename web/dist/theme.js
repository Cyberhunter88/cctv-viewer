/* Runs before styles to avoid flashing the wrong color scheme. */
(() => {
  const key = 'cctv-theme';
  const system = matchMedia('(prefers-color-scheme: dark)');
  let choice = 'system';
  try { const saved = localStorage.getItem(key); if (['system', 'light', 'dark'].includes(saved)) choice = saved; } catch {}
  function apply() {
    document.documentElement.dataset.theme = choice === 'system' ? (system.matches ? 'dark' : 'light') : choice;
    document.querySelectorAll('[data-theme-select]').forEach(select => select.value = choice);
    window.dispatchEvent(new Event('themechange'));
  }
  apply();
  system.addEventListener('change', () => { if (choice === 'system') apply(); });
  addEventListener('storage', event => {
    if (event.key !== key) return;
    choice = ['light', 'dark'].includes(event.newValue) ? event.newValue : 'system';
    const select = document.getElementById('theme'); if (select) select.value = choice;
    apply();
  });
  addEventListener('DOMContentLoaded', () => {
    document.querySelectorAll('[data-theme-select]').forEach(select => {
      select.value = choice;
      select.addEventListener('change', () => {
        choice = select.value;
        try { localStorage.setItem(key, choice); } catch {}
        apply();
      });
    });
  });
})();
