import { cp, mkdir, readFile, readdir, writeFile, stat } from 'node:fs/promises';
import { resolve, relative } from 'node:path';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';

const root = resolve(import.meta.dirname, '../..');
const version = process.argv[2];
if (!version || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$/.test(version)) {
  throw new Error('Usage: node scripts/release/package.mjs VERSION');
}
const target = resolve(root, 'build/releases', version);
await mkdir(resolve(root, 'build/releases'), { recursive: true });
await mkdir(target, { recursive: false });
for (const [from, to] of [
  ['build/blog', 'blog'],
  ['build/publisher', 'publisher'],
  ['frontend/dist', 'frontend'],
  ['build/notices', 'notices'],
  ['deploy', 'deploy'],
  ['docs', 'docs'],
  ['scripts/backup-daily.sh', 'scripts/backup-daily.sh'],
  ['scripts/backup-retain.sh', 'scripts/backup-retain.sh'],
  ['README.md', 'README.md'],
  ['README.ar.md', 'README.ar.md'],
  ['LICENSE', 'LICENSE'],
  ['CONTRIBUTING.md', 'CONTRIBUTING.md'],
  ['CONTRIBUTING.ar.md', 'CONTRIBUTING.ar.md'],
  ['SECURITY.md', 'SECURITY.md'],
  ['SECURITY.ar.md', 'SECURITY.ar.md'],
  ['.env.example', '.env.example'],
]) {
  await cp(resolve(root, from), resolve(target, to), { recursive: true });
}
const manifest = {
  version,
  platform: process.platform,
  arch: process.arch,
  node: process.version,
  go: execFileSync('go', ['version'], { cwd: resolve(root, 'backend'), encoding: 'utf8' }).trim(),
  files: [],
};
async function walk(directory) {
  for (const name of (await readdir(directory)).sort()) {
    const path = resolve(directory, name);
    if ((await stat(path)).isDirectory()) {
      await walk(path);
    } else {
      const bytes = await readFile(path);
      manifest.files.push({
        path: relative(target, path),
        bytes: bytes.length,
        sha256: createHash('sha256').update(bytes).digest('hex'),
      });
    }
  }
}
await walk(target);
await writeFile(resolve(target, 'release.json'), JSON.stringify(manifest, null, 2) + '\n');
execFileSync('tar', ['-czf', target + '.tar.gz', '-C', resolve(root, 'build/releases'), version]);
console.log('Release packaged: ' + relative(root, target + '.tar.gz'));
