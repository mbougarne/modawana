import assert from 'node:assert/strict';

// Each confirmation belongs to one action; never leave an accepting listener behind.
export async function confirmAction(page, message, action) {
  assert.equal(page.listenerCount('dialog'), 0, 'Another dialog handler is already installed');
  let timer;
  let handle;
  const confirmation = new Promise((resolve, reject) => {
    timer = setTimeout(() => reject(new Error('Expected confirmation did not appear')), 30000);
    handle = (dialog) => {
      Promise.resolve()
        .then(async () => {
          if (dialog.type() !== 'confirm' || dialog.message() !== message) {
            await dialog.dismiss();
            throw new Error('Unexpected confirmation for this smoke action');
          }
          await dialog.accept();
        })
        .then(resolve, reject);
    };
    page.once('dialog', handle);
  });
  try {
    await Promise.all([confirmation, Promise.resolve().then(action)]);
  } finally {
    clearTimeout(timer);
    page.off('dialog', handle);
  }
}
