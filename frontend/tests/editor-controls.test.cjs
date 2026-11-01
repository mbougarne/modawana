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
const { EditorTabs } = require('../.test-output/ui/src/editor/EditorTabs');
const { EditorTags } = require('../.test-output/ui/src/editor/EditorTags');
const { MarkdownToolbar } = require('../.test-output/ui/src/editor/MarkdownToolbar');
const { editorStatistics } = require('../.test-output/ui/src/editor/markdown');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
async function mount(component, locale = 'en') {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  await act(async () => root.render(React.createElement(I18nProvider, { locale }, component)));
  return {
    el,
    cleanup: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}
async function key(node, value) {
  await act(async () =>
    node.dispatchEvent(
      new dom.window.KeyboardEvent('keydown', { key: value, bubbles: true, cancelable: true }),
    ),
  );
}
async function input(node, value) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, 'value').set.call(
      node,
      value,
    );
    node.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
  });
}
const tabs = [
  { key: 'write', label: 'Write' },
  { key: 'preview', label: 'Preview' },
  { key: 'media', label: 'Media', count: 2 },
];
function TabHarness() {
  const [current, setCurrent] = React.useState('write');
  return React.createElement(
    React.Fragment,
    {},
    React.createElement(EditorTabs, { current, tabs, onChange: setCurrent }),
    React.createElement('textarea', {
      hidden: current !== 'write',
      defaultValue: 'A محفوظ draft',
      'data-draft': true,
    }),
  );
}

test('Editor tabs support arrows/Home/End, roving focus and unchanged draft DOM across views', async () => {
  const m = await mount(React.createElement(TabHarness));
  try {
    const buttons = m.el.querySelectorAll('[role="tab"]');
    const draft = m.el.querySelector('textarea');
    buttons[0].focus();
    await key(buttons[0], 'ArrowRight');
    assert.equal(document.activeElement, buttons[1]);
    assert.equal(buttons[1].getAttribute('aria-selected'), 'true');
    assert.equal(buttons[1].tabIndex, 0);
    assert.equal(buttons[0].tabIndex, -1);
    assert.equal(draft.hidden, true);
    await key(buttons[1], 'End');
    assert.equal(document.activeElement, buttons[2]);
    assert.equal(buttons[2].getAttribute('aria-label'), 'Media');
    await key(buttons[2], 'Home');
    assert.equal(document.activeElement, buttons[0]);
    assert.equal(m.el.querySelector('textarea'), draft);
    assert.equal(draft.value, 'A محفوظ draft');
    assert.equal(draft.hidden, false);
  } finally {
    await m.cleanup();
  }
});

test('Editor tab arrow navigation mirrors in an Arabic interface', async () => {
  const m = await mount(React.createElement(TabHarness), 'ar');
  try {
    const buttons = m.el.querySelectorAll('[role="tab"]');
    buttons[0].focus();
    await key(buttons[0], 'ArrowLeft');
    assert.equal(document.activeElement, buttons[1]);
    await key(buttons[1], 'ArrowRight');
    assert.equal(document.activeElement, buttons[0]);
  } finally {
    await m.cleanup();
  }
});

async function tags(initial = []) {
  let latest = initial;
  function Harness() {
    const [value, setValue] = React.useState(initial);
    return React.createElement(EditorTags, {
      value,
      terms: [
        { key: 'tag_go', label: 'غو' },
        { key: 'tag_ts', label: 'TypeScript' },
      ],
      onChange: (next) => {
        latest = next;
        setValue(next);
      },
    });
  }
  return { ...(await mount(React.createElement(Harness))), value: () => latest };
}
test('Tag chips filter localized names and retain stable keys when adding/removing', async () => {
  const m = await tags();
  try {
    await input(m.el.querySelector('input'), 'غو');
    await key(m.el.querySelector('input'), 'Enter');
    assert.deepEqual(m.value(), ['tag_go']);
    assert.equal(m.el.querySelector('.vb-tok bdi').textContent, 'غو');
    await act(async () => m.el.querySelector('.vb-tok button').click());
    assert.deepEqual(m.value(), []);
  } finally {
    await m.cleanup();
  }
});

test('Tag picker keeps existing unknown keys and prevents a ninth tag', async () => {
  const initial = Array.from({ length: 8 }, (_, i) => 'existing-' + i);
  const m = await tags(initial);
  try {
    assert.equal(m.el.querySelector('.vb-suggest button').disabled, true);
    await act(async () => m.el.querySelector('.vb-tok button').click());
    await act(async () => m.el.querySelector('.vb-suggest button').click());
    assert.equal(m.value().length, 8);
    assert.ok(m.value().includes('tag_go'));
    assert.equal(initial.length, 8);
    assert.equal(m.el.querySelector('.vb-suggest button').disabled, true);
  } finally {
    await m.cleanup();
  }
});

test('Editor reading estimate agrees with saved metadata at empty and 200-word boundaries in EN/AR', () => {
  assert.deepEqual(editorStatistics(' \n\t '), { words: 0, minutes: 0 });
  assert.deepEqual(editorStatistics(Array(200).fill('word').join(' ')), { words: 200, minutes: 1 });
  assert.deepEqual(editorStatistics(Array(201).fill('كلمة').join('\n')), {
    words: 201,
    minutes: 2,
  });
});

test('Formatting toolbar delegates actions without inline style attributes or losing mouse selection', async () => {
  const edits = [];
  const names = [
    'h2',
    'h3',
    'bold',
    'italic',
    'code',
    'link',
    'quote',
    'ul',
    'ol',
    'codeblock',
    'table',
    'hr',
    'image',
    'video',
    'split',
  ];
  const m = await mount(
    React.createElement(MarkdownToolbar, {
      target: { current: null },
      label: 'Formatting',
      labels: Object.fromEntries(names.map((x) => [x, x])),
      onEdit: (action) => edits.push(action),
      onUpload: () => {},
      onToggleSplit: () => {},
    }),
  );
  try {
    assert.equal(m.el.querySelector('[style]'), null);
    const bold = m.el.querySelector('[aria-label="bold"]');
    const event = new dom.window.MouseEvent('mousedown', { bubbles: true, cancelable: true });
    await act(async () => bold.dispatchEvent(event));
    assert.equal(event.defaultPrevented, true);
    await act(async () => bold.click());
    assert.deepEqual(edits, ['bold']);
  } finally {
    await m.cleanup();
  }
});

test('A thousand tags stay bounded to five suggestions and all remain searchable', async () => {
  let selected;
  const m = await mount(
    React.createElement(EditorTags, {
      value: [],
      terms: Array.from({ length: 1000 }, (_, n) => ({ key: 'tag-' + n, label: 'Topic ' + n })),
      onChange: (keys) => {
        selected = keys;
      },
    }),
  );
  try {
    assert.equal(m.el.querySelectorAll('.vb-suggest button').length, 5);
    await input(m.el.querySelector('input'), 'Topic 999');
    assert.equal(m.el.querySelectorAll('.vb-suggest button').length, 1);
    await key(m.el.querySelector('input'), 'Enter');
    assert.deepEqual(selected, ['tag-999']);
  } finally {
    await m.cleanup();
  }
});
