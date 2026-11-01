const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>');
global.window = dom.window;
global.document = dom.window.document;
global.HTMLElement = dom.window.HTMLElement;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const ui = '../.test-output/ui/src/';
const { I18nProvider } = require(ui + 'i18n/I18nProvider');
const { DEFAULT_LABELS } = require(ui + 'i18n/labels');
const { StableKeyField, generateStableKey } = require(ui + 'components/StableKeyField');
// Resolve the application's Webpack alias to the same compiled kit for this test.
const Module = require('node:module');
const originalLoad = Module._load;
Module._load = function (name, ...args) {
  if (name === '@ui') {
    return {
      ...require(ui + 'components/primitives'),
      ...require(ui + 'components/Icon'),
      ...require(ui + 'components/learning'),
      ...require(ui + 'types'),
      ...require(ui + 'i18n/I18nProvider'),
    };
  }
  return originalLoad.call(this, name, ...args);
};
const { QuizBuilder } = require('../.test-output/frontend/src/pages/admin/editor/QuizBuilder');
Module._load = originalLoad;
const t = (key) => DEFAULT_LABELS[key].en;
async function mount(child, locale = 'en') {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  await act(async () => root.render(React.createElement(I18nProvider, { locale }, child)));
  return {
    el,
    close: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}
async function click(el) {
  await act(async () => el.click());
}
const button = (el, name) =>
  [...el.querySelectorAll('button')].find(
    (b) => b.textContent.trim() === name || b.getAttribute('aria-label') === name,
  );

test('Generated keys fit content/taxonomy/collection constraints; click changes only editable working key', async () => {
  const values = new Set(Array.from({ length: 100 }, generateStableKey));
  assert.equal(values.size, 100);
  for (const key of values) {
    assert.match(key, /^key-[a-f0-9-]{36}$/);
    assert.ok(key.length <= 80);
  }
  let current = 'manual-key';
  function Form() {
    const [value, setValue] = React.useState(current);
    return React.createElement(StableKeyField, {
      value,
      onChange: (v) => {
        current = v;
        setValue(v);
      },
    });
  }
  const m = await mount(React.createElement(Form), 'ar');
  const generate = m.el.querySelector('button');
  assert.equal(generate.textContent, '');
  assert.equal(generate.getAttribute('aria-label'), DEFAULT_LABELS['owner.generateKey'].ar);
  await click(generate);
  assert.match(current, /^key-/);
  assert.equal(document.activeElement, m.el.querySelector('input'));
  assert.equal(m.el.querySelector('input').value, current);
  await m.close();
  const locked = await mount(
    React.createElement(StableKeyField, {
      value: 'saved-key',
      disabled: true,
      onChange: () => assert.fail('locked key changed'),
    }),
  );
  assert.equal(locked.el.querySelector('button'), null);
  assert.equal(locked.el.querySelector('input').disabled, true);
  assert.equal(locked.el.querySelector('input').value, 'saved-key');
  await locked.close();
});

const question = (id) => ({
  id,
  type: 'single',
  text: 'Question',
  options: [
    { id: 'a', text: 'A' },
    { id: 'b', text: 'B' },
  ],
  correct: ['a'],
  explanation: '',
});
function Harness({ translation = false, initial, changed }) {
  const [value, setValue] = React.useState(initial);
  return React.createElement(QuizBuilder, {
    value,
    onChange: (v) => {
      setValue(v);
      changed(v);
    },
    dirty: true,
    csrf: '',
    contentLocale: 'en',
    translation,
    onRefreshSource: () => {},
  });
}
test('Question cards add each type, retain IDs on reorder, mark invalid cards and preserve canonical changes', async () => {
  let value;
  const m = await mount(
    React.createElement(Harness, {
      initial: {
        assessment_id: 'saved-assessment',
        pass_mark: 1,
        show_explanations: true,
        questions: [question('first')],
      },
      changed: (v) => {
        value = v;
      },
    }),
  );
  assert.equal(m.el.querySelector('.vb-qcard').dataset.invalid, 'false');
  for (const type of ['multi', 'bool']) {
    await click(button(m.el, '+ ' + t('builder.' + type)));
  }
  assert.deepEqual(
    value.questions.map((q) => q.type),
    ['single', 'multi', 'bool'],
  );
  assert.equal(value.assessment_id, undefined);
  assert.equal(m.el.querySelectorAll('.vb-qcard[data-invalid="true"]').length, 2);
  const lastId = value.questions[2].id;
  await click(button(m.el.querySelectorAll('.quiz-question-actions')[2], t('owner.up')));
  assert.equal(value.questions[1].id, lastId);
  assert.deepEqual(
    value.questions[1].options.map((o) => o.id),
    ['true', 'false'],
  );
  assert.equal(button(m.el, t('builder.savePreview')).disabled, true);
  await m.close();
});

test('Translations keep assessment structure locked while allowing localized wording', async () => {
  const m = await mount(
    React.createElement(Harness, {
      translation: true,
      initial: {
        assessment_id: 'source',
        pass_mark: 1,
        show_explanations: true,
        questions: [question('q')],
      },
      changed: () => {},
    }),
    'ar',
  );
  for (const el of m.el.querySelectorAll(
    '.quiz-question-actions button, .quiz-answer-actions button, .vb-qb-add button, select, input[type="radio"], input[type="number"]',
  )) {
    assert.equal(el.disabled, true);
  }
  assert.equal(m.el.querySelector('textarea').disabled, false);
  await m.close();
});

const definition = () => ({
  assessment_id: 'assessment-new',
  pass_mark: 1,
  show_explanations: true,
  questions: [question('fresh-q')],
});
function savedDraft(kind = 'article') {
  return {
    key: 'fresh',
    kind,
    locale: 'en',
    collection_key: kind === 'lesson' ? 'course' : '',
    version: 7,
    revision: { revision_id: 'revision-new', quiz: definition() },
  };
}
function SavedActions({ initialBase, save, publish }) {
  const [base, setBase] = React.useState(initialBase);
  const [dirty, setDirty] = React.useState(!initialBase);
  return React.createElement(QuizBuilder, {
    value: definition(),
    base,
    dirty,
    csrf: 'csrf',
    contentLocale: 'en',
    contentKind: initialBase?.kind || 'article',
    translation: false,
    onChange: () => {},
    onRefreshSource: () => {},
    onSave: async () => {
      const d = await save();
      if (d) {
        setBase(d);
        setDirty(false);
      }
      return d;
    },
    onPublishPlan: publish,
  });
}
test('Save and try previews the freshly saved quiz and grades privately with its exact version', async () => {
  const calls = [];
  global.fetch = async (url, options) => {
    calls.push({ url, options });
    const body = JSON.parse(options.body);
    return {
      ok: true,
      json: async () => ({
        version: body.version,
        passed: true,
        score: 1,
        total: 1,
        feedback: { 'fresh-q': { correct: true } },
      }),
    };
  };
  let saves = 0;
  const m = await mount(
    React.createElement(SavedActions, {
      save: async () => {
        saves++;
        return savedDraft();
      },
    }),
  );
  await click(button(m.el, t('builder.savePreview')));
  assert.equal(saves, 1);
  assert.ok(m.el.querySelector('.vb-quiz'));
  await click(m.el.querySelector('.vb-quiz input[type="radio"]'));
  await click(m.el.querySelector('.vb-quiz button.vb-btn-primary'));
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/v1/content/fresh/en/quiz-preview');
  assert.equal(calls[0].options.headers['X-CSRF-Token'], 'csrf');
  assert.deepEqual(JSON.parse(calls[0].options.body).version, {
    quiz: 'assessment-new',
    content: 'revision-new',
  });
  assert.deepEqual(JSON.parse(calls[0].options.body).answers, { 'fresh-q': ['a'] });
  await m.close();
});
test('Assessment review saves first and sends the exact returned revision and version', async () => {
  const calls = [];
  global.fetch = async (url, options) => {
    calls.push({ url, options });
    return { ok: true, json: async () => ({ reviewed: true }) };
  };
  const m = await mount(React.createElement(SavedActions, { save: async () => savedDraft() }));
  await click(button(m.el, t('builder.saveReview')));
  assert.equal(calls[0].url, '/api/v1/content/fresh/en/quiz-review');
  assert.deepEqual(JSON.parse(calls[0].options.body), {
    expected_version: 7,
    revision_id: 'revision-new',
  });
  assert.match(m.el.querySelector('[role="status"]').textContent, /reviewed/);
  await m.close();
});
test('Cancelled or rejected saves never review an old quiz and report actionable errors', async () => {
  const { AuthError } = require('../.test-output/frontend/src/features/auth/api');
  global.fetch = async () => assert.fail('no quiz request after save rejection');
  for (const save of [
    async () => undefined,
    async () => {
      throw new AuthError('forbidden');
    },
  ]) {
    const m = await mount(React.createElement(SavedActions, { save }));
    await click(button(m.el, t('builder.saveReview')));
    const notice = m.el.querySelector('[role="status"]');
    assert.equal(notice.dataset.tone, 'error');
    assert.ok(
      notice.textContent === t('builder.saveNeeded') || notice.textContent === t('request.confirm'),
    );
    assert.equal(m.el.querySelector('.vb-quiz'), null);
    await m.close();
  }
});
test('Gate setup prepares the first private plan, explains missing review and offers explicit publication', async () => {
  const calls = [];
  let plan = '';
  let reviewed = false;
  let enabled = false;
  let published = 0;
  global.fetch = async (url, options) => {
    calls.push({ url, options });
    let result = {};
    if (url === '/api/v1/collections/course') {
      result = {
        key: 'course',
        kind: 'learning',
        version: 4,
        draft_plan_id: plan,
        draft_members: plan ? [{ key: 'fresh' }, { key: 'later' }] : [],
        assigned_members: [
          { key: 'fresh', announcements: {} },
          { key: 'later', announcements: {} },
        ],
      };
    } else if (url.endsWith('/plan')) {
      plan = 'private-plan';
      result = { version: 5, plan_id: plan };
    } else if (url.endsWith('/quiz-review')) {
      reviewed = true;
      result = { reviewed: true };
    } else if (url.endsWith('/quiz-gate') && options.method === 'PUT') {
      const payload = JSON.parse(options.body);
      assert.equal(payload.plan_id, 'private-plan');
      assert.equal(payload.assessment_id, 'assessment-new');
      if (!reviewed) {
        return {
          ok: false,
          status: 422,
          json: async () => ({
            error: {
              code: 'invalid_fields',
              fields: [{ path: 'quiz', code: 'assessment_review' }],
            },
          }),
        };
      }
      enabled = payload.enabled;
    } else if (url.endsWith('/quiz-gate')) {
      result = { key: 'course', version: plan ? 5 : 4, plan_id: plan, enabled };
    } else {
      assert.fail(url);
    }
    return { ok: true, json: async () => result };
  };
  const m = await mount(
    React.createElement(SavedActions, {
      initialBase: savedDraft('lesson'),
      publish: async () => {
        published++;
      },
    }),
  );
  await click(button(m.el, t('builder.addGate')));
  assert.equal(enabled, false);
  assert.equal(published, 0);
  assert.equal(m.el.querySelector('[role="status"]').textContent, t('builder.reviewNeeded'));
  await click(button(m.el, t('builder.review')));
  await click(button(m.el, t('builder.addGate')));
  assert.equal(enabled, true);
  assert.equal(published, 0);
  assert.ok(button(m.el, t('builder.removeGate')));
  await click(button(m.el, t('progress.publishPlan')));
  assert.equal(published, 1);
  assert.equal(calls.filter((c) => c.url.endsWith('/plan')).length, 1);
  await m.close();
});

test('A delayed initial path load cannot overwrite the newly saved gate state', async () => {
  let release;
  let first = true;
  let enabled = false;
  global.fetch = async (url, options) => {
    if (url.endsWith('/quiz-gate') && options.method === 'GET' && first) {
      first = false;
      return new Promise((resolve) => {
        release = () =>
          resolve({
            ok: true,
            json: async () => ({ key: 'course', version: 1, plan_id: 'plan', enabled: false }),
          });
      });
    }
    let result;
    if (url === '/api/v1/collections/course') {
      result = {
        key: 'course',
        kind: 'learning',
        version: 2,
        draft_plan_id: 'plan',
        draft_members: [{ key: 'fresh' }, { key: 'later' }],
        assigned_members: [{ key: 'fresh' }],
      };
    } else if (options.method === 'PUT') {
      enabled = true;
      result = {};
    } else {
      result = { key: 'course', version: 2, plan_id: 'plan', enabled };
    }
    return { ok: true, json: async () => result };
  };
  const m = await mount(React.createElement(SavedActions, { initialBase: savedDraft('lesson') }));
  await click(button(m.el, t('builder.addGate')));
  assert.ok(button(m.el, t('builder.removeGate')));
  await act(async () => release());
  assert.ok(button(m.el, t('builder.removeGate')));
  await m.close();
});
