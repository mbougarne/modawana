import { readdir, readFile, mkdir, cp, writeFile, access, rm } from 'node:fs/promises';
import { resolve, basename } from 'node:path';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';

const root = resolve(import.meta.dirname, '../..');
const output = resolve(root, 'build/notices');
// Rebuild generated notices so removed dependencies cannot leak into later packages.
await rm(output, { recursive: true, force: true });
await mkdir(output, { recursive: true });
const inventory = [];
async function collect(kind, name, version, directory, license) {
  const id = `${kind}-${name.replace(/[^a-zA-Z0-9._-]/g, '_')}@${version}`;
  const target = resolve(output, id);
  await mkdir(target, { recursive: true });
  const names = (await readdir(directory)).filter((file) =>
    /^(licen[sc]e|copying|notice|ofl)(?:$|[.-])/i.test(file),
  );
  if (!names.length) {
    const readme = (await readdir(directory)).find((file) => /^readme(?:$|[.-])/i.test(file));
    if (readme) {
      names.push(readme);
    }
  }
  const files = [];
  for (const name of names.sort()) {
    const bytes = await readFile(resolve(directory, name)).catch(() => null);
    if (!bytes) {
      continue;
    }
    await writeFile(resolve(target, name), bytes);
    files.push({ file: `${id}/${name}`, sha256: createHash('sha256').update(bytes).digest('hex') });
  }
  if (!files.length) {
    throw new Error('No upstream notice found for ' + name);
  }
  inventory.push({ ecosystem: kind, name, version, declared_license: license ?? null, files });
}
const lock = JSON.parse(await readFile(resolve(root, 'frontend/package-lock.json'), 'utf8'));
for (const [path, pkg] of Object.entries(lock.packages)) {
  if (!path || pkg.link) {
    continue;
  }
  const directory = resolve(root, 'frontend', path);
  try {
    await access(directory);
  } catch {
    if (pkg.optional) {
      continue;
    }
    throw new Error('Missing installed dependency ' + path);
  }
  const details = JSON.parse(await readFile(resolve(directory, 'package.json'), 'utf8'));
  await collect('npm', details.name ?? basename(path), details.version, directory, details.license);
}
if (!process.argv.includes('--npm-only')) {
  const stream = execFileSync('go', ['list', '-m', '-json', 'all'], {
    cwd: resolve(root, 'backend'),
    encoding: 'utf8',
  });
  const modules = JSON.parse('[' + stream.trim().replace(/}\s*\n{/g, '},{') + ']');
  for (const module of modules.filter((module) => !module.Main)) {
    if (!module.Dir) {
      throw new Error('Missing downloaded module ' + module.Path);
    }
    await collect('go', module.Path, module.Version, module.Dir);
  }
  const goroot = execFileSync('go', ['env', 'GOROOT'], {
    cwd: resolve(root, 'backend'),
    encoding: 'utf8',
  }).trim();
  await cp(resolve(goroot, 'LICENSE'), resolve(output, 'Go-LICENSE.txt'));
}
await cp(resolve(root, 'ui/src/fonts/LICENSES'), resolve(output, 'fonts'), { recursive: true });
await cp(resolve(root, 'LICENSE'), resolve(output, 'ENGINE-MIT.txt'));
await writeFile(resolve(output, 'inventory.json'), JSON.stringify(inventory, null, 2) + '\n');
console.log(
  `Collected upstream notices for ${inventory.length} installed dependencies plus fonts, Go (when enabled), and engine provenance.`,
);
