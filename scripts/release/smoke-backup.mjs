import { mkdtemp, mkdir, writeFile, readFile, readdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import assert from 'node:assert/strict';

if (process.platform !== 'linux') {
  throw new Error('Backup operator tests require the supported Linux/GNU environment.');
}
const root = resolve(import.meta.dirname, '../..');
const work = await mkdtemp(resolve(tmpdir(), 'blog-backup-runner-'));
try {
  const backups = resolve(work, 'backups');
  const bin = resolve(work, 'bin');
  await mkdir(bin);
  for (const folder of ['daily', 'weekly']) {
    await mkdir(resolve(backups, folder), { recursive: true });
  }
  for (let day = 1; day <= 12; day++) {
    await writeFile(
      resolve(backups, 'daily', `2026-10-${String(day).padStart(2, '0')}.zip.age`),
      'encrypted fixture',
    );
  }
  for (let week = 1; week <= 6; week++) {
    await writeFile(
      resolve(backups, 'weekly', `2026-W${String(week).padStart(2, '0')}.zip.age`),
      'encrypted fixture',
    );
  }
  await writeFile(resolve(backups, 'daily/keep-me.zip.age'), 'unrelated');
  const mock = async (name, code) =>
    writeFile(resolve(bin, name), '#!/bin/sh\nset -eu\n' + code + '\n', { mode: 0o700 });
  await mock('blog', 'printf snapshot > "$3"');
  await mock('age', 'cp "$5" "$4"');
  await mock('ssh', 'exit 0');
  await mock('rsync', 'exit 1');
  const env = {
    ...process.env,
    PATH: bin + ':' + process.env.PATH,
    BLOG_BINARY: resolve(bin, 'blog'),
    BLOG_BACKUP_DIR: backups,
    BLOG_BACKUP_REMOTE: 'backup@host:/private/blog',
    BLOG_BACKUP_RECIPIENT: 'public-test-recipient',
  };
  let result = spawnSync('sh', ['scripts/backup-daily.sh'], { cwd: root, env, encoding: 'utf8' });
  assert.notEqual(result.status, 0, 'failed transfer must fail the job');
  assert.equal(
    await readFile(resolve(backups, 'daily/2026-10-01.zip.age'), 'utf8'),
    'encrypted fixture',
    'failed transfer must not prune old backups',
  );
  assert.equal(
    (await readdir(backups)).some((name) => name.startsWith('.pending-')),
    false,
    'plaintext temporary archive removed',
  );
  result = spawnSync('sh', ['scripts/backup-retain.sh', backups], { cwd: root, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(
    (await readdir(resolve(backups, 'daily'))).filter((n) => /^\d{4}-/.test(n)).length,
    7,
  );
  assert.equal((await readdir(resolve(backups, 'weekly'))).length, 4);
  assert.equal(await readFile(resolve(backups, 'daily/keep-me.zip.age'), 'utf8'), 'unrelated');
  result = spawnSync('sh', ['scripts/backup-daily.sh'], {
    cwd: root,
    env: { ...env, BLOG_BACKUP_REMOTE: '-oProxyCommand@host:/private' },
  });
  assert.equal(result.status, 2, 'unsafe SSH target rejected');
  console.log(
    'Linux retention, failed-transfer preservation, plaintext cleanup and SSH target validation passed; network/encryption commands were simulated.',
  );
} finally {
  await rm(work, { recursive: true, force: true });
}
