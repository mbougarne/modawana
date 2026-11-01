// Markdown parity harness for ADR 0002 stage 1: renders the Go renderer's
// fixture suite (backend/internal/render/testdata/fixtures) through the real
// browser pipeline (MarkdownReader) and compares it with the committed Go
// goldens after the documented normalizations below. Run from the repository
// root with `node scripts/markdown-parity.mjs`; it needs frontend/node_modules
// (node scripts/project.mjs install). This is a development/test tool only,
// never a production service.
//
// Normalizations (deliberate, documented divergences between the pipelines):
// 1. DOM-level equivalence: attribute order, attribute-name case, entity
//    encodings and self-closing slashes compare equal; whitespace-only text
//    nodes are dropped and runs of whitespace collapse outside pre/code.
// 2. Math: the browser renders KaTeX MathML, the server emits the TeX source
//    in marked .vb-math elements for the KaTeX island. Both reduce to the TeX
//    source plus the display flag before comparing.
// 3. Code copy chrome: the browser's CodeBlock header (filename bar and Copy
//    button) is client-side chrome and is removed; interactivity returns as
//    an island in later stages.
// 4. Language classes: the Go renderer keeps language-* classes on fenced
//    code for themes and highlight islands; the browser CodeBlock drops them.
// 5. Task-list classes: remark-gfm adds contains-task-list/task-list-item
//    classes; goldmark does not, and no stylesheet targets them.
// 6. Table alignment: the browser emits style="text-align:…", the server
//    emits align attributes because CSP forbids inline styles.
import { spawnSync } from 'node:child_process';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const frontend = resolve(root, 'frontend');
const fixturesDir = resolve(root, 'backend/internal/render/testdata/fixtures');
const requireFrontend = createRequire(resolve(frontend, 'package.json'));

const compiled = [
  resolve(frontend, '.test-output/frontend/src/features/markdown/MarkdownReader.js'),
  resolve(frontend, '.test-output/ui/src/i18n/I18nProvider.js'),
];
if (!compiled.every((path) => existsSync(path))) {
  const tsc = spawnSync(
    process.execPath,
    [resolve(frontend, 'node_modules/typescript/bin/tsc'), '-p', 'tsconfig.test.json'],
    { cwd: frontend, stdio: 'inherit' },
  );
  if (tsc.status !== 0) {
    console.error('markdown-parity: compiling frontend test output failed');
    process.exit(1);
  }
}

const React = requireFrontend('react');
const { renderToStaticMarkup } = requireFrontend('react-dom/server');
const { JSDOM } = requireFrontend('jsdom');
const { I18nProvider } = requireFrontend(compiled[1]);
const { MarkdownReader } = requireFrontend(compiled[0]);

const voidTags = new Set([
  'area',
  'base',
  'br',
  'col',
  'embed',
  'hr',
  'img',
  'input',
  'link',
  'meta',
  'source',
  'track',
  'wbr',
]);

function escapeHtml(value) {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');
}

function serialize(node, preformatted) {
  if (node.nodeType === 3) {
    let text = node.data;
    if (!preformatted) {
      if (!text.trim()) {
        return '';
      }
      text = text.replace(/\s+/g, ' ');
    }
    return escapeHtml(text);
  }
  if (node.nodeType !== 1) {
    return '';
  }
  const tag = node.tagName.toLowerCase();
  const attributes = [...node.attributes]
    .map((a) => [a.name, a.value])
    .sort((a, b) => (a[0] < b[0] ? -1 : 1))
    .map(([name, value]) => ` ${name}="${escapeHtml(value)}"`)
    .join('');
  let out = `<${tag}${attributes}>`;
  if (voidTags.has(tag)) {
    return out;
  }
  const keepSpace = preformatted || tag === 'pre' || tag === 'code' || tag === 'x-math';
  for (const child of node.childNodes) {
    out += serialize(child, keepSpace);
  }
  return `${out}</${tag}>`;
}

function stripClasses(element, shouldDrop) {
  for (const token of [...element.classList]) {
    if (shouldDrop(token)) {
      element.classList.remove(token);
    }
  }
  if (!element.className) {
    element.removeAttribute('class');
  }
}

function normalize(html) {
  const document = new JSDOM(`<body>${html}</body>`).window.document;
  for (const header of document.querySelectorAll('figure.vb-code > header')) {
    header.remove();
  }
  for (const element of document.querySelectorAll('span.katex')) {
    const annotation = element.querySelector('annotation[encoding="application/x-tex"]');
    const math = document.createElement('x-math');
    math.setAttribute(
      'display',
      String(element.querySelector('math')?.getAttribute('display') === 'block'),
    );
    math.textContent = annotation ? annotation.textContent : '';
    element.replaceWith(math);
  }
  for (const element of document.querySelectorAll('.vb-math')) {
    const math = document.createElement('x-math');
    math.setAttribute('display', String(element.getAttribute('data-math') === 'display'));
    math.textContent = element.textContent;
    element.replaceWith(math);
  }
  for (const code of document.querySelectorAll('figure.vb-code code')) {
    stripClasses(code, (token) => token.startsWith('language-'));
  }
  for (const element of document.querySelectorAll('.contains-task-list, .task-list-item')) {
    stripClasses(element, (token) => token === 'contains-task-list' || token === 'task-list-item');
  }
  for (const cell of document.querySelectorAll('th[style], td[style]')) {
    const align = /text-align:\s*(left|right|center)/.exec(cell.getAttribute('style'));
    if (align) {
      cell.setAttribute('align', align[1]);
    }
    cell.removeAttribute('style');
  }
  let out = '';
  for (const child of document.body.childNodes) {
    out += serialize(child, false);
  }
  return out;
}

function renderBrowser(markdown, config) {
  return renderToStaticMarkup(
    React.createElement(
      I18nProvider,
      { locale: config.locale ?? 'en' },
      React.createElement(MarkdownReader, {
        markdown,
        media: config.media ?? [],
        presentations: config.presentations ?? [],
        takeaway: config.takeaway || undefined,
      }),
    ),
  );
}

function firstDifference(a, b) {
  let i = 0;
  while (i < a.length && i < b.length && a[i] === b[i]) {
    i += 1;
  }
  const from = Math.max(0, i - 80);
  return [`  browser: …${a.slice(from, i + 120)}…`, `  server:  …${b.slice(from, i + 120)}…`].join(
    '\n',
  );
}

const names = readdirSync(fixturesDir)
  .filter((name) => name.endsWith('.md'))
  .map((name) => name.replace(/\.md$/, ''))
  .sort();
if (names.length === 0) {
  console.error('markdown-parity: no fixtures found');
  process.exit(1);
}
let failures = 0;
for (const name of names) {
  const markdown = readFileSync(resolve(fixturesDir, `${name}.md`), 'utf8');
  const goldenPath = resolve(fixturesDir, `${name}.html`);
  if (!existsSync(goldenPath)) {
    console.error(`${name}: missing golden; run go test ./internal/render -update in backend/`);
    failures += 1;
    continue;
  }
  const configPath = resolve(fixturesDir, `${name}.json`);
  const config = existsSync(configPath) ? JSON.parse(readFileSync(configPath, 'utf8')) : {};
  const browser = normalize(renderBrowser(markdown, config));
  const server = normalize(readFileSync(goldenPath, 'utf8'));
  if (browser === server) {
    console.log(`ok ${name}`);
  } else {
    failures += 1;
    console.error(`FAIL ${name}: pipelines diverge`);
    console.error(firstDifference(browser, server));
  }
}
if (failures > 0) {
  console.error(`markdown-parity: ${failures} of ${names.length} fixtures diverge`);
  process.exit(1);
}
console.log(`markdown-parity: ${names.length} fixtures match`);
