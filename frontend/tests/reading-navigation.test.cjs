const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const {
  observeReadingProgress,
} = require('../.test-output/frontend/src/islands/readingNavigation');

test('reader progress updates navigation from the latest Go response and preserves the quiz', async (t) => {
  const dom = new JSDOM(
    `<body data-rendered="server">
      <aside data-learning-navigation="outline">Locked upcoming lesson</aside>
      <nav data-learning-navigation="neighbors">Locked next lesson</nav>
      <div data-reader-learning><span>Passed result</span></div>
    </body>`,
    { url: 'https://blog.test/en/learn/path/first' },
  );
  const { window } = dom;
  for (const key of ['window', 'document', 'DOMParser']) {
    const prior = Object.getOwnPropertyDescriptor(globalThis, key);
    Object.defineProperty(globalThis, key, { configurable: true, value: window[key] });
    t.after(() => {
      if (prior) {
        Object.defineProperty(globalThis, key, prior);
      } else {
        delete globalThis[key];
      }
    });
  }
  t.after(() => dom.window.close());
  const requests = [];
  t.mock.method(globalThis, 'fetch', (url, options) => {
    return new Promise((resolve) => requests.push({ url, options, resolve }));
  });
  const quiz = window.document.querySelector('[data-reader-learning]');
  observeReadingProgress();
  window.dispatchEvent(new window.Event('learning-progress'));
  window.dispatchEvent(new window.Event('learning-progress'));

  const [old, current] = requests;
  assert.equal(old.options.signal.aborted, true);
  assert.equal(current.url, window.location.href);
  assert.equal(current.options.cache, 'no-store');
  current.resolve({
    ok: true,
    text: async () =>
      `<body data-rendered="server"><aside data-learning-navigation="outline">Unlocked</aside>
      <nav data-learning-navigation="neighbors"><a href="/en/learn/path/next">Next</a></nav>
      <div data-reader-learning>Empty</div></body>`,
  });
  old.resolve({
    ok: true,
    text: async () =>
      '<body data-rendered="server"><aside data-learning-navigation="outline">Obsolete</aside></body>',
  });
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(window.document.querySelector('aside').textContent, 'Unlocked');
  assert.equal(window.document.querySelector('nav a').getAttribute('href'), '/en/learn/path/next');
  assert.equal(window.document.querySelector('[data-reader-learning]'), quiz);
  assert.equal(quiz.textContent, 'Passed result');
});
