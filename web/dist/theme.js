/* Runs before styles to avoid flashing the wrong color scheme. */
(() => {
  const key = 'cctv-theme';
  const system = matchMedia('(prefers-color-scheme: dark)');
  let choice = 'system';
  try { const saved = localStorage.getItem(key); if (['system', 'light', 'dark'].includes(saved)) choice = saved; } catch {}
  function apply() {
    document.documentElement.dataset.theme = choice === 'system' ? (system.matches ? 'dark' : 'light') : choice;
    document.querySelectorAll('[data-theme-select]').forEach(select => {
      if (select.id === 'theme' && select.closest('.content-page')?.querySelector('form')?.dataset.draftActive === 'true') return;
      select.value = choice;
    });
    window.dispatchEvent(new Event('themechange'));
  }
  apply();
  system.addEventListener('change', () => { if (choice === 'system') apply(); });
  addEventListener('storage', event => {
    if (event.key !== key) return;
    choice = ['light', 'dark'].includes(event.newValue) ? event.newValue : 'system';
    apply();
  });
  addEventListener('themecommit', event => {
    if (!['system', 'light', 'dark'].includes(event.detail)) return;
    choice = event.detail;
    try { localStorage.setItem(key, choice); } catch {}
    apply();
  });
  addEventListener('DOMContentLoaded', () => {
    document.querySelectorAll('[data-theme-select]').forEach(select => {
      select.value = choice;
      if (select.id === 'theme') return; // Personal settings are committed by their form.
      select.addEventListener('change', () => {
        choice = select.value;
        try { localStorage.setItem(key, choice); } catch {}
        apply();
      });
    });
  });
})();
