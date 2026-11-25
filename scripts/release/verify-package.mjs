import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFile, readdir, stat } from 'node:fs/promises';
import { basename, resolve } from 'node:path';

async function verifyPackage(input, work) {
  let source = input;
  if ((await stat(input)).isFile()) {
    const digest = createHash('sha256')
      .update(await readFile(input))
      .digest('hex');
    assert.equal(await readFile(input + '.sha256', 'utf8'), digest + '  ' + basename(input) + '\n');
    execFileSync('tar', ['-xzf', input, '-C', work]);
    const entries = await readdir(work);
    assert.equal(entries.length, 1);
    source = resolve(work, entries[0]);
  }
  const manifest = JSON.parse(await readFile(resolve(source, 'release.json'), 'utf8'));
  for (const file of manifest.files) {
    const bytes = await readFile(resolve(source, file.path));
    assert.equal(bytes.length, file.bytes, file.path);
    assert.equal(createHash('sha256').update(bytes).digest('hex'), file.sha256, file.path);
  }
  for (const file of [
    'examples/catalog-spec.json',
    'examples/plugins/example/plugin.wasm',
    'examples/themes/pack-example/theme.json',
    'docs/self-hosted-release.md',
  ]) {
    assert.ok(
      manifest.files.some((entry) => entry.path === file),
      file,
    );
  }
  return { source, manifest };
}

export { verifyPackage };
