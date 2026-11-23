import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

import { developmentDataRoot } from './development-data.mjs';

test('development commands share one persistent default and honor an explicit data directory', (t) => {
  const temporary = mkdtempSync(join(tmpdir(), 'modawana-data-test-'));
  t.after(() => rmSync(temporary, { recursive: true, force: true }));
  const home = join(temporary, 'home');
  const persistent = join(home, '.local', 'share', 'modawana-dev');
  const legacy = join(temporary, 'modawana-dev-a0123456789b');
  assert.equal(developmentDataRoot(undefined, home, temporary), persistent);
  mkdirSync(legacy);
  assert.equal(developmentDataRoot(undefined, home, temporary), persistent);
  writeFileSync(join(legacy, 'blog.sqlite'), '');
  assert.throws(() => developmentDataRoot(undefined, home, temporary), {
    message: /Found earlier Modawana development data.*\n.*modawana-dev-a0123456789b/,
  });
  assert.equal(developmentDataRoot(legacy, home, temporary), legacy);
  mkdirSync(persistent, { recursive: true });
  writeFileSync(join(persistent, 'blog.sqlite'), '');
  assert.equal(developmentDataRoot(undefined, home, temporary), persistent);
});
