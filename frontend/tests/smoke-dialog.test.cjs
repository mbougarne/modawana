const { test } = require('node:test');
const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');

function dialog(message, error) {
  const result = { accepted: 0, dismissed: 0 };
  return {
    result,
    type: () => 'confirm',
    message: () => message,
    accept: async () => {
      result.accepted++;
      if (error) {
        throw error;
      }
    },
    dismiss: async () => {
      result.dismissed++;
    },
  };
}

test('Sequential smoke confirmations are handled exactly once and leave no listeners', async () => {
  const { confirmAction } = await import('../../scripts/smoke-dialog.mjs');
  const page = new EventEmitter();
  for (const message of ['Discard edits?', 'Convert content?', 'Publish plan?']) {
    const current = dialog(message);
    await confirmAction(page, message, async () => page.emit('dialog', current));
    assert.deepEqual(current.result, { accepted: 1, dismissed: 0 });
    assert.equal(page.listenerCount('dialog'), 0);
  }
});

test('Smoke confirmations reject competing handlers and clean up failed actions', async () => {
  const { confirmAction } = await import('../../scripts/smoke-dialog.mjs');
  const page = new EventEmitter();
  const existing = () => {};
  page.on('dialog', existing);
  let acted = false;
  await assert.rejects(
    confirmAction(page, 'Confirm?', async () => {
      acted = true;
    }),
    /already installed/,
  );
  assert.equal(acted, false);
  page.off('dialog', existing);
  await assert.rejects(
    confirmAction(page, 'Confirm?', async () => {
      throw new Error('Button missing');
    }),
    /Button missing/,
  );
  assert.equal(page.listenerCount('dialog'), 0);
});

test('Unexpected dialogs are dismissed and accept errors are awaited', async () => {
  const { confirmAction } = await import('../../scripts/smoke-dialog.mjs');
  const page = new EventEmitter();
  const unexpected = dialog('Wrong operation');
  await assert.rejects(
    confirmAction(page, 'Expected operation', async () => page.emit('dialog', unexpected)),
    /Unexpected confirmation/,
  );
  assert.deepEqual(unexpected.result, { accepted: 0, dismissed: 1 });
  const failed = dialog('Expected operation', new Error('Dialog closed'));
  await assert.rejects(
    confirmAction(page, 'Expected operation', async () => page.emit('dialog', failed)),
    /Dialog closed/,
  );
  assert.equal(page.listenerCount('dialog'), 0);
});
