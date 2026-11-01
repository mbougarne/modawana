// KaTeX island for server-rendered pages (ADR 0002): the Go renderer emits
// TeX sources in marked .vb-math elements and the host includes this bundle
// only on pages that contain math. The KaTeX options mirror MarkdownReader's
// rehype-katex configuration so both pipelines produce MathML with the same
// bounds. On a rendering error the readable TeX source is left in place.
import katex from 'katex';

function renderMath() {
  for (const element of Array.from(document.querySelectorAll<HTMLElement>('.vb-math'))) {
    const tex = element.textContent ?? '';
    try {
      katex.render(tex, element, {
        displayMode: element.getAttribute('data-math') === 'display',
        output: 'mathml',
        trust: false,
        strict: 'error',
        maxExpand: 1000,
        maxSize: 20,
      });
    } catch {
      /* Keep the TeX source readable. */
    }
  }
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', renderMath);
} else {
  renderMath();
}
