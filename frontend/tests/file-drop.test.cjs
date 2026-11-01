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
const { FileDropInput } = require('../.test-output/ui/src/components/FileDropInput');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
async function mount(extra = {}) {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  const files = [];
  await act(async () =>
    root.render(
      React.createElement(
        I18nProvider,
        { locale: 'en' },
        React.createElement(FileDropInput, {
          label: 'Upload',
          title: 'Drop media',
          help: 'Supported media',
          accept: 'image/png,video/mp4',
          onFile: (file) => files.push(file),
          ...extra,
        }),
      ),
    ),
  );
  return {
    el,
    files,
    cleanup: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}

test('Media picker buttons select the requested image/video filter and pass the chosen file to one upload handler', async () => {
  const m = await mount({
    choices: [
      { label: 'Choose image', accept: 'image/png', icon: 'image' },
      { label: 'Choose video', accept: 'video/mp4', icon: 'video' },
    ],
  });
  try {
    const picker = m.el.querySelector('input[type="file"]');
    const opened = [];
    picker.click = () => opened.push(picker.accept);
    assert.equal(m.el.querySelectorAll('input[type="file"]').length, 1);
    assert.equal(m.el.querySelector('label'), null);
    await act(async () => m.el.querySelectorAll('button')[0].click());
    await act(async () => m.el.querySelectorAll('button')[1].click());
    assert.deepEqual(opened, ['image/png', 'video/mp4']);
    const file = { name: 'clip.mp4' };
    Object.defineProperty(picker, 'files', { configurable: true, value: [file] });
    await act(async () => picker.dispatchEvent(new dom.window.Event('change', { bubbles: true })));
    assert.deepEqual(m.files, [file]);
    assert.equal(picker.value, '');
  } finally {
    await m.cleanup();
  }
});

test('Generic picker has an explicit keyboard button; dropped files share the callback and disabled drops do nothing', async () => {
  const m = await mount();
  try {
    assert.equal(m.el.querySelector('button').textContent, 'Choose file');
    const event = new dom.window.Event('drop', { bubbles: true, cancelable: true });
    const file = { name: 'image.png' };
    Object.defineProperty(event, 'dataTransfer', { value: { files: [file] } });
    await act(async () => m.el.querySelector('[role="group"]').dispatchEvent(event));
    assert.deepEqual(m.files, [file]);
  } finally {
    await m.cleanup();
  }
  const disabled = await mount({ disabled: true });
  try {
    assert.equal(disabled.el.querySelector('button').disabled, true);
    const event = new dom.window.Event('drop', { bubbles: true, cancelable: true });
    Object.defineProperty(event, 'dataTransfer', { value: { files: [{ name: 'ignored.png' }] } });
    await act(async () => disabled.el.querySelector('[role="group"]').dispatchEvent(event));
    assert.deepEqual(disabled.files, []);
  } finally {
    await disabled.cleanup();
  }
});
