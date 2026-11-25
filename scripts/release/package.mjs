import { cp, mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import { resolve, relative, basename } from 'node:path';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { writeTarGzip } from './tar.mjs';

const root = resolve(import.meta.dirname, '../..');
const version = process.argv[2];
if (!version || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$/.test(version)) {
  throw new Error('Usage: node scripts/release/package.mjs VERSION [OUTPUT_DIRECTORY]');
}
const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' });
const sourceCommit = git('rev-parse', 'HEAD').trim();
const sourceDirty = git('status', '--porcelain').length > 0;
const documentation = git(
  'ls-files',
  '-z',
  '--cached',
  '--others',
  '--exclude-standard',
  '--',
  'deploy',
  'docs',
)
  .split('\0')
  .filter(Boolean);
const output = process.argv[3] ? resolve(process.argv[3]) : resolve(root, 'build/releases');
const target = resolve(output, version);
const archive = target + '.tar.gz';
await mkdir(output, { recursive: true });
await mkdir(target, { recursive: false });
for (const [from, to] of [
  ['build/blog', 'blog'],
  ['build/publisher', 'publisher'],
  ['frontend/dist', 'frontend'],
  ['build/notices', 'notices'],
  ...documentation.map((path) => [path, path]),
  ['examples/catalog-spec.json', 'examples/catalog-spec.json'],
  ['examples/themes/pack-example/theme.json', 'examples/themes/pack-example/theme.json'],
  [
    'backend/internal/plugin/testdata/example/manifest.json',
    'examples/plugins/example/manifest.json',
  ],
  ['examples/plugins/typescript/assembly', 'examples/plugins/typescript/assembly'],
  ['examples/plugins/typescript/contributions', 'examples/plugins/typescript/contributions'],
  ['examples/plugins/typescript/manifest.json', 'examples/plugins/typescript/manifest.json'],
  ['examples/plugins/typescript/package.json', 'examples/plugins/typescript/package.json'],
  [
    'examples/plugins/typescript/package-lock.json',
    'examples/plugins/typescript/package-lock.json',
  ],
  ['scripts/release/catalog.mjs', 'scripts/release/catalog.mjs'],
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
execFileSync(process.execPath, [
  resolve(root, 'scripts/build-example-plugin.mjs'),
  resolve(target, 'examples/plugins/example/plugin.wasm'),
]);
const manifest = {
  version,
  platform: process.platform,
  arch: process.arch,
  node: process.version,
  go: execFileSync('go', ['version'], { cwd: resolve(root, 'backend'), encoding: 'utf8' }).trim(),
  source_commit: sourceCommit,
  source_dirty: sourceDirty,
  files: [],
};
async function walk(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  entries.sort((a, b) => (a.name < b.name ? -1 : Number(a.name > b.name)));
  for (const entry of entries) {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) {
      await walk(path);
    } else if (entry.isFile()) {
      const bytes = await readFile(path);
      manifest.files.push({
        path: relative(target, path),
        bytes: bytes.length,
        sha256: createHash('sha256').update(bytes).digest('hex'),
      });
    } else {
      throw new Error('Release entry must be a regular file: ' + entry.name);
    }
  }
}
await walk(target);
await writeFile(resolve(target, 'release.json'), JSON.stringify(manifest, null, 2) + '\n');
await writeTarGzip(output, version, archive);
const hash = createHash('sha256');
for await (const chunk of createReadStream(archive)) {
  hash.update(chunk);
}
await writeFile(archive + '.sha256', hash.digest('hex') + '  ' + basename(archive) + '\n', {
  flag: 'wx',
});
console.log('Release packaged: ' + relative(root, archive));
