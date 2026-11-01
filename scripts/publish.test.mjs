import assert from 'node:assert/strict';
import { chmodSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { loadPublisherConfig, runPublisher } from './publish.mjs';

function fixture(
  t,
  contents = 'BLOG_URL=https://blog.example\nBLOG_PUBLISHER_TOKEN="test-only-secret"\n',
) {
  const directory = mkdtempSync(join(tmpdir(), 'blog-publisher-launcher-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const file = join(directory, 'publisher.env');
  writeFileSync(file, contents, { mode: 0o600 });
  return { directory, file };
}

test('configuration is parsed as data; shell substitutions are never executed', (t) => {
  const { directory, file } = fixture(t);
  const marker = join(directory, 'unexpected');
  writeFileSync(file, `BLOG_URL=https://blog.example\nBLOG_PUBLISHER_TOKEN='$(touch ${marker})'\n`);
  assert.equal(loadPublisherConfig(file).BLOG_PUBLISHER_TOKEN, `$(touch ${marker})`);
  assert.throws(() => loadPublisherConfig(marker), /configuration rejected/);
});

test('permissions, symlinks, unexpected variables and unsafe origins fail without echoing secrets', (t) => {
  const { directory, file } = fixture(t);
  chmodSync(file, 0o644);
  assert.throws(() => loadPublisherConfig(file), /configuration rejected/);
  chmodSync(file, 0o600);
  const link = join(directory, 'linked.env');
  symlinkSync(file, link);
  assert.throws(() => loadPublisherConfig(link), /configuration rejected/);
  for (const contents of [
    'BLOG_URL=http://blog.example\nBLOG_PUBLISHER_TOKEN=test-only-secret',
    'BLOG_URL=https://test-only-secret@blog.example\nBLOG_PUBLISHER_TOKEN=test-only-secret',
    'BLOG_URL=https://blog.example/path\nBLOG_PUBLISHER_TOKEN=test-only-secret',
    'BLOG_URL=https://blog.example\nBLOG_PUBLISHER_TOKEN="test-only-secret\nsecond-line"',
    'BLOG_URL=https://blog.example\nBLOG_PUBLISHER_TOKEN=test-only-secret\nNODE_OPTIONS=--inspect',
    'BLOG_URL=https://blog.example\nBLOG_PUBLISHER_TOKEN=',
    'BLOG_URL=https://blog.example\nBLOG_PUBLISHER_TOKEN=PASTE_YOUR_BLOG_TOKEN_HERE',
  ]) {
    writeFileSync(file, contents);
    assert.throws(
      () => loadPublisherConfig(file),
      (error) => {
        assert.match(error.message, /configuration rejected/);
        assert.equal(error.message.includes('test-only-secret'), false);
        return true;
      },
    );
  }
});

test('launcher passes credentials only through the child environment and preserves arguments/status', (t) => {
  const { directory, file } = fixture(t);
  const binary = join(directory, 'publisher');
  writeFileSync(
    binary,
    '#!/bin/sh\n' +
      '[ "$BLOG_URL" = "https://blog.example" ] || exit 10\n' +
      '[ "$BLOG_PUBLISHER_TOKEN" = "test-only-secret" ] || exit 11\n' +
      '[ "$#" = 3 ] || exit 12\n' +
      '[ "$1" = get ] && [ "$2" = --key ] && [ "$3" = lesson-001 ] || exit 13\n' +
      'exit 7\n',
    { mode: 0o700 },
  );
  assert.equal(
    runPublisher(['get', '--key', 'lesson-001'], {
      file,
      binary,
      environment: { BLOG_PUBLISHER_TOKEN: 'stale-inherited-value' },
      stdio: 'pipe',
    }),
    7,
  );
});

test('local check does not start the publisher', (t) => {
  const { directory, file } = fixture(t);
  const binary = join(directory, 'publisher');
  writeFileSync(binary, '#!/bin/sh\nexit 99\n', { mode: 0o700 });
  assert.equal(runPublisher(['--check'], { file, binary }), 0);
});

test('an explicit configuration path preserves existing private installations', (t) => {
  const { directory, file } = fixture(t);
  const binary = join(directory, 'publisher');
  writeFileSync(binary, '#!/bin/sh\nexit 99\n', { mode: 0o700 });
  const environment = { BLOG_PUBLISHER_CONFIG: file };
  assert.equal(runPublisher(['--check'], { binary, environment }), 0);
  chmodSync(file, 0o644);
  assert.throws(() => runPublisher(['--check'], { binary, environment }), /configuration rejected/);
});
