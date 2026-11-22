// Reuse the Go outline and neighbor rendering after progress changes. Keep
// the quiz and its submitted result mounted while the page updates around it.
export function observeReadingProgress() {
  const selector = '[data-learning-navigation]';
  if (!document.querySelector(selector)) {
    return;
  }
  let pending: AbortController | undefined;
  window.addEventListener('learning-progress', async () => {
    pending?.abort();
    const request = new AbortController();
    pending = request;
    try {
      const response = await fetch(window.location.href, {
        signal: request.signal,
        cache: 'no-store',
        credentials: 'same-origin',
        redirect: 'error',
      });
      if (!response.ok) {
        return;
      }
      const html = await response.text();
      if (request.signal.aborted) {
        return;
      }
      const next = new DOMParser().parseFromString(html, 'text/html');
      if (next.body.dataset.rendered !== 'server') {
        return;
      }
      const fragments = new Map(
        Array.from(next.querySelectorAll<HTMLElement>(selector), (element) => [
          element.dataset.learningNavigation,
          element,
        ]),
      );
      for (const current of document.querySelectorAll<HTMLElement>(selector)) {
        const updated = fragments.get(current.dataset.learningNavigation);
        if (updated && !current.isEqualNode(updated)) {
          current.replaceWith(updated);
        }
      }
    } catch {
      // The last rendered outline remains usable. The next full load refreshes it.
    } finally {
      if (pending === request) {
        pending = undefined;
      }
    }
  });
  window.addEventListener('pagehide', () => pending?.abort());
}
