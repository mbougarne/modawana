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
const { BrandColourField } = require('../.test-output/ui/src/components/BrandColourField');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
async function mount(role) {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  let latest = '#123456';
  function Harness() {
    const [value, setValue] = React.useState(latest);
    return React.createElement(
      I18nProvider,
      { locale: 'en' },
      React.createElement(BrandColourField, {
        role,
        value,
        onChange: (v) => {
          latest = v;
          setValue(v);
        },
      }),
    );
  }
  await act(async () => root.render(React.createElement(Harness)));
  return {
    el,
    value: () => latest,
    cleanup: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
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

test('Brand colours expose the five approved presets for each role and synchronize picker, hex and selection', async () => {
  for (const [role, expected] of [
    ['primary', ['#2B44C4', '#B23F26', '#1D6B58', '#7A3E9D', '#15161A']],
    ['secondary', ['#F2B33D', '#F28C6B', '#8FD3C1', '#F5B8D0', '#C9E265']],
  ]) {
    const m = await mount(role);
    try {
      const buttons = [...m.el.querySelectorAll('button')];
      assert.equal(buttons.length, 5);
      for (const [index, hex] of expected.entries()) {
        await act(async () => buttons[index].click());
        assert.equal(m.value(), hex);
        assert.equal(m.el.querySelector('input[type="text"]').value, hex);
        assert.equal(m.el.querySelector('input[type="color"]').value.toUpperCase(), hex);
        assert.equal(m.el.querySelectorAll('[aria-pressed="true"]').length, 1);
      }
      await input(m.el.querySelector('input[type="text"]'), '#abcdef');
      assert.equal(m.value(), '#ABCDEF');
      assert.equal(m.el.querySelectorAll('[aria-pressed="true"]').length, 0);
    } finally {
      await m.cleanup();
    }
  }
});

test('Invalid hex remains visible for correction instead of silently replacing the working value', async () => {
  const m = await mount('primary');
  try {
    await input(m.el.querySelector('input[type="text"]'), '#12');
    assert.equal(m.value(), '#12');
    assert.equal(m.el.querySelector('input[type="text"]').getAttribute('aria-invalid'), 'true');
    assert.ok(m.el.querySelector('[role="alert"]'));
    await input(m.el.querySelector('input[type="text"]'), '#123abc');
    assert.equal(m.value(), '#123ABC');
    assert.equal(m.el.querySelector('[role="alert"]'), null);
  } finally {
    await m.cleanup();
  }
});
