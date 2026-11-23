import { existsSync, readdirSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { resolve } from 'node:path';

export function developmentDataRoot(configured, home = homedir(), temporary = tmpdir()) {
  if (configured) {
    return configured;
  }
  const persistent = resolve(home, '.local/share/modawana-dev');
  if (existsSync(resolve(persistent, 'blog.sqlite'))) {
    return persistent;
  }
  const legacy = readdirSync(temporary, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && /^modawana-dev-[a-f0-9]{12}$/.test(entry.name))
    .map((entry) => resolve(temporary, entry.name))
    .filter((path) => existsSync(resolve(path, 'blog.sqlite')))
    .sort();
  if (legacy.length) {
    throw new Error(
      'Found earlier Modawana development data. Set BLOG_DATA_ROOT to your existing data directory before starting:\n' +
        legacy.join('\n') +
        '\nSee docs/getting-started.md for moving it to the persistent default.',
    );
  }
  return persistent;
}
