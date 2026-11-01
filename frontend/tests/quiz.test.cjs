const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>', { url: 'http://localhost/' });
global.window = dom.window;
global.document = dom.window.document;
global.HTMLElement = dom.window.HTMLElement;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { LessonQuiz } = require('../.test-output/ui/src/components/learning');
const { QuizError } = require('../.test-output/ui/src/types');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const version = { quiz: 'quiz-1', content: 'content-1', path: 'path-1', epoch: 1 };
const quiz = {
  id: 'one',
  version,
  locale: 'en',
  passMark: 1,
  unlocksNext: true,
  showExplanations: true,
  questions: [
    {
      id: 'q',
      type: 'single',
      text: 'Choose a value',
      options: [
        { id: 'a', text: 'A' },
        { id: 'b', text: 'B' },
      ],
    },
  ],
};
const answer = (passed) => ({
  version,
  score: passed ? 1 : 0,
  total: 1,
  passed,
  feedback: {
    q: { correct: passed, correctOptionIds: ['a'], explanation: 'Only returned after grading.' },
  },
  unlockedHref: passed ? '/en/learn/path/next' : undefined,
});
async function mount(props) {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  const render = async (next) =>
    act(async () =>
      root.render(
        React.createElement(I18nProvider, { locale: 'en' }, React.createElement(LessonQuiz, next)),
      ),
    );
  await render(props);
  return {
    el,
    render,
    cleanup: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}
async function click(el) {
  assert.ok(el, 'element exists');
  await act(async () => el.click());
}
const button = (el, text) =>
  [...el.querySelectorAll('button')].find((b) => b.textContent.includes(text));

test('network failure preserves choices and operation key; feedback appears only after a successful response', async () => {
  const calls = [];
  let fail = true;
  const m = await mount({
    quiz,
    onReload() {},
    onSubmit: async (request) => {
      calls.push(request);
      if (fail) {
        throw new Error('private server detail');
      }
      return answer(true);
    },
  });
  try {
    assert.doesNotMatch(m.el.textContent, /Only returned/);
    await click(button(m.el, 'Check answers'));
    assert.match(m.el.textContent, /every question|all.*question|Answer/i);
    assert.equal(calls.length, 0);
    await click(m.el.querySelector('input'));
    await click(button(m.el, 'Check answers'));
    assert.match(m.el.textContent, /Could not submit/);
    assert.doesNotMatch(m.el.textContent, /private server detail/);
    assert.equal(m.el.querySelector('input').checked, true);
    fail = false;
    await click(button(m.el, 'Check answers'));
    assert.equal(calls[0].idempotencyKey, calls[1].idempotencyKey);
    assert.deepEqual(calls[0].version, version);
    assert.match(m.el.textContent, /Only returned after grading/);
    assert.equal(m.el.querySelector('a').getAttribute('href'), '/en/learn/path/next');
  } finally {
    await m.cleanup();
  }
});

test('failed grade can be retried; a new attempt clears answers and gets a new key', async () => {
  const calls = [];
  const m = await mount({
    quiz,
    onReload() {},
    onSubmit: async (request) => {
      calls.push(request);
      return answer(false);
    },
  });
  try {
    await click(m.el.querySelectorAll('input')[1]);
    await click(button(m.el, 'Check answers'));
    assert.equal(m.el.querySelector('a'), null);
    await click(button(m.el, 'Try again'));
    assert.equal(m.el.querySelector('input:checked'), null);
    assert.doesNotMatch(m.el.textContent, /Only returned/);
    await click(m.el.querySelector('input'));
    await click(button(m.el, 'Check answers'));
    assert.notEqual(calls[0].idempotencyKey, calls[1].idempotencyKey);
  } finally {
    await m.cleanup();
  }
});

test('pending answers are disabled and stale versions require reload, not another submission', async () => {
  let complete;
  let reloads = 0;
  let calls = 0;
  const m = await mount({
    quiz,
    onReload() {
      reloads++;
    },
    onSubmit: () => {
      calls++;
      return new Promise((resolve) => {
        complete = resolve;
      });
    },
  });
  try {
    await click(m.el.querySelector('input'));
    await click(button(m.el, 'Check answers'));
    assert.equal(m.el.querySelector('fieldset').disabled, true);
    assert.match(m.el.textContent, /Checking answers/);
    await act(async () => complete({ ...answer(true), version: { ...version, quiz: 'new' } }));
    assert.match(m.el.textContent, /has changed/);
    assert.equal(m.el.querySelector('a'), null);
    await click(button(m.el, 'Reload quiz'));
    assert.equal(reloads, 1);
    assert.equal(calls, 1);
  } finally {
    await m.cleanup();
  }
});

test('new version discards obsolete async result; loading and API errors are rendered', async () => {
  let complete;
  const props = {
    quiz,
    onReload() {},
    onSubmit: () =>
      new Promise((resolve) => {
        complete = resolve;
      }),
  };
  const m = await mount(props);
  try {
    await click(m.el.querySelector('input'));
    await click(button(m.el, 'Check answers'));
    await m.render({ ...props, quiz: { ...quiz, version: { ...version, quiz: 'new' } } });
    await act(async () => complete(answer(true)));
    assert.equal(m.el.querySelector('a'), null);
    assert.equal(m.el.querySelector('input:checked'), null);
    await m.render({ ...props, quiz: null });
    assert.match(m.el.textContent, /Loading the quiz/);
    await m.render({ ...props, quiz: null, loadError: new QuizError('unavailable') });
    assert.match(m.el.textContent, /unavailable/);
    await m.render({ ...props, quiz: null, loadError: new QuizError('rate_limited', 30) });
    assert.match(m.el.textContent, /Too many attempts/);
  } finally {
    await m.cleanup();
  }
});
