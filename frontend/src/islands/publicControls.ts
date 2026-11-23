const appearance = document.querySelector<HTMLButtonElement>('[data-theme-toggle]');
if (appearance) {
  appearance.hidden = false;
  appearance.addEventListener('click', () => {
    const root = document.documentElement;
    const current =
      root.dataset.theme || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
    const next = current === 'dark' ? 'light' : 'dark';
    root.dataset.theme = next;
    try {
      localStorage.setItem('theme', next);
    } catch {
      /* The current page still uses the selected appearance. */
    }
  });
}

const copyEmail = document.querySelector<HTMLButtonElement>('[data-copy-email]');
const copyStatus = document.querySelector<HTMLElement>('[data-copy-status]');
if (copyEmail && copyStatus && navigator.clipboard) {
  copyEmail.hidden = false;
  copyEmail.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(copyEmail.dataset.copyEmail ?? '');
      copyStatus.textContent = copyEmail.dataset.copied ?? '';
    } catch {
      copyStatus.textContent = copyEmail.dataset.copyFailed ?? '';
    }
  });
}

const menus = document.querySelectorAll<HTMLDetailsElement>('.public-tools .public-details');
for (const menu of menus) {
  const trigger = menu.querySelector('summary');
  if (!trigger) {
    continue;
  }
  trigger.setAttribute('aria-expanded', String(menu.open));
  menu.addEventListener('toggle', () => {
    trigger.setAttribute('aria-expanded', String(menu.open));
    if (menu.open) {
      for (const other of menus) {
        if (other !== menu) {
          other.open = false;
        }
      }
      menu.querySelector('a')?.focus();
    }
  });
  menu.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && menu.open) {
      event.preventDefault();
      menu.open = false;
      trigger.focus();
    }
  });
  document.addEventListener('pointerdown', (event) => {
    if (event.target instanceof Node && !menu.contains(event.target)) {
      menu.open = false;
    }
  });
}

function enableLiveSearch(form: HTMLFormElement, results: HTMLElement, status: HTMLElement) {
  const input = form.querySelector<HTMLInputElement>('input[type="search"]');
  if (!input) {
    return;
  }
  const shortcut = form.querySelector('kbd');
  if (shortcut) {
    shortcut.hidden = false;
  }
  let pending: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout>;
  function cancel() {
    clearTimeout(timer);
    pending?.abort();
    pending = undefined;
    results.removeAttribute('aria-busy');
  }
  async function refresh(url: URL) {
    const controller = new AbortController();
    pending = controller;
    results.setAttribute('aria-busy', 'true');
    try {
      const response = await fetch(url, {
        signal: controller.signal,
        cache: 'no-store',
        credentials: 'same-origin',
        redirect: 'error',
      });
      if (!response.ok) {
        throw new Error('Search request failed');
      }
      const html = await response.text();
      if (pending !== controller) {
        return;
      }
      const updated = new DOMParser()
        .parseFromString(html, 'text/html')
        .querySelector('[data-discovery-results]');
      if (!updated) {
        throw new Error('Search results unavailable');
      }
      history.replaceState(history.state, '', url);
      results.replaceChildren(...updated.children);
    } catch {
      if (!controller.signal.aborted) {
        status.textContent = form.dataset.liveError ?? '';
        status.hidden = false;
      }
    } finally {
      if (pending === controller) {
        pending = undefined;
        results.removeAttribute('aria-busy');
      }
    }
  }
  function schedule() {
    cancel();
    status.hidden = true;
    const url = new URL(form.action);
    for (const [key, value] of new FormData(form)) {
      if (typeof value === 'string' && value.trim()) {
        url.searchParams.set(key, key === 'q' ? value.trim() : value);
      }
    }
    const current = new URL(location.href);
    current.searchParams.sort();
    url.searchParams.sort();
    if (url.search !== current.search) {
      timer = setTimeout(() => void refresh(url), 250);
    }
  }
  input.addEventListener('input', schedule);
  form.querySelector('select')?.addEventListener('change', schedule);
  form.addEventListener('submit', cancel);
  window.addEventListener('pagehide', cancel);
  document.addEventListener('keydown', (event) => {
    const target = event.target;
    if (
      event.key === '/' &&
      target instanceof HTMLElement &&
      !target.closest('input, textarea, select, [contenteditable]')
    ) {
      event.preventDefault();
      input.focus();
    }
  });
}

const search = document.querySelector<HTMLFormElement>('[data-live-search]');
const results = document.querySelector<HTMLElement>('[data-discovery-results]');
const liveStatus = document.querySelector<HTMLElement>('[data-live-status]');
if (search && results && liveStatus) {
  enableLiveSearch(search, results, liveStatus);
}

export {};

for (const form of document.querySelectorAll<HTMLFormElement>('form[data-lead-form]')) {
  const status = form.querySelector<HTMLElement>('[role="status"]');
  const fieldset = form.querySelector('fieldset');
  if (!status || !fieldset || fieldset.disabled) {
    continue;
  }
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    if (fieldset.disabled) {
      return;
    }
    const data = new URLSearchParams();
    for (const [key, value] of new FormData(form)) {
      if (typeof value === 'string') {
        data.append(key, value);
      }
    }
    fieldset.disabled = true;
    try {
      const response = await fetch(form.action, {
        method: 'POST',
        body: data,
        credentials: 'same-origin',
        redirect: 'error',
      });
      if (!response.ok) {
        throw new Error('Submission failed');
      }
      form.reset();
      status.textContent = form.dataset.accepted ?? '';
    } catch {
      status.textContent = form.dataset.failed ?? '';
      fieldset.disabled = false;
    }
  });
}
