const { test } = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const Module = require('node:module');
const resolve = Module._resolveFilename;
Module._resolveFilename = function (request, ...args) {
  return resolve.call(
    this,
    request === '@ui' ? path.resolve(__dirname, '../.test-output/ui/src/index.js') : request,
    ...args,
  );
};
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<html><body></body></html>');
global.window = dom.window;
global.document = dom.window.document;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { MonthPicker } = require('../.test-output/frontend/src/features/configuration/MonthPicker');
const { portraitCrop } = require('../.test-output/frontend/src/features/configuration/portrait');

test('Portrait crop centers landscape/portrait images and bounds output without upscaling', () => {
  assert.deepEqual(portraitCrop(1024, 1536), { x: 0, y: 256, side: 1024, size: 1024 });
  assert.deepEqual(portraitCrop(2400, 1600), { x: 400, y: 0, side: 1600, size: 1200 });
  assert.throws(() => portraitCrop(500, 900));
  assert.throws(() => portraitCrop(6000, 6000));
});
test('Month picker selects and clears YYYY-MM values in EN/AR/FR and Escape restores focus', async () => {
  for (const locale of ['en', 'ar', 'fr']) {
    const el = document.createElement('div');
    document.body.append(el);
    const root = createRoot(el);
    let selected;
    function Harness() {
      const [value, setValue] = React.useState('2024-10');
      return React.createElement(MonthPicker, {
        name: 'config.updatedMonth',
        value,
        onChange: (v) => {
          selected = v;
          setValue(v);
        },
      });
    }
    try {
      await act(async () =>
        root.render(React.createElement(I18nProvider, { locale }, React.createElement(Harness))),
      );
      const trigger = el.querySelector('button');
      await act(async () => trigger.click());
      assert.equal(el.querySelectorAll('.config-month-grid button').length, 12);
      await act(async () => el.querySelectorAll('.config-month-grid button')[1].click());
      assert.equal(selected, '2024-02');
      assert.equal(document.activeElement, trigger);
      await act(async () => trigger.click());
      await act(async () => el.querySelector('.config-month-panel > button').click());
      assert.equal(selected, '');
      await act(async () => trigger.click());
      await act(async () =>
        el
          .querySelector('input')
          .dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
      );
      assert.equal(el.querySelector('.config-month-panel'), null);
      assert.equal(document.activeElement, trigger);
    } finally {
      await act(async () => root.unmount());
      el.remove();
    }
  }
});
