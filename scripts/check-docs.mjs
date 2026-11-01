import { readdirSync, readFileSync, existsSync, writeFileSync } from 'node:fs';
import { join, dirname, relative, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
// testdata holds Go test fixtures (Markdown as input data), not documentation.
const excluded = new Set([
  '.git',
  'node_modules',
  'dist',
  'build',
  'coverage',
  'generated_images',
  'testdata',
  '_docs',
]);
function walk(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    if (excluded.has(e.name) || (dir === root && e.name === 'internal')) {
      return [];
    }
    const p = join(dir, e.name);
    return e.isDirectory() ? walk(p) : e.name.endsWith('.md') ? [relative(root, p)] : [];
  });
}
const errors = [];
const inventory = walk(root)
  .sort()
  .map((path) => {
    if (path.startsWith('content/')) {
      return { path, coverage: 'legacy-content-not-documentation' };
    }
    const text = readFileSync(join(root, path), 'utf8');
    let coverage;
    let companion;
    if (/^## English\s*$/m.test(text) && /^## العربية\s*$/m.test(text)) {
      coverage = 'bilingual';
      if (!text.includes('](#english)') || !text.includes('](#العربية)')) {
        errors.push(`${path}: missing language navigation`);
      }
    } else {
      companion = path.endsWith('.ar.md')
        ? path.replace(/\.ar\.md$/, '.md')
        : path.replace(/\.md$/, '.ar.md');
      coverage = path.endsWith('.ar.md') ? 'ar' : 'en';
      if (!existsSync(join(root, companion))) {
        errors.push(`${path}: missing ${companion}`);
      }
      if (!text.includes(companion.split('/').at(-1) + ')')) {
        errors.push(`${path}: missing companion link`);
      }
    }
    const prose = text.replace(/```[\s\S]*?```/g, '');
    for (const match of prose.matchAll(/\[[^\]]*\]\(([^)]+)\)/g)) {
      const target = match[1].split('#')[0];
      if (!target || /^(?:[a-z]+:|\/)/i.test(target)) {
        continue;
      }
      if (!existsSync(resolve(root, dirname(path), decodeURIComponent(target)))) {
        errors.push(`${path}: broken link ${target}`);
      }
    }
    return { path, coverage, ...(companion ? { companion } : {}) };
  });
const output = JSON.stringify({ documents: inventory }, null, 2) + '\n';
const inventoryPath = join(root, 'docs/documentation-inventory.json');
if (process.argv.includes('--write')) {
  writeFileSync(inventoryPath, output);
} else if (!existsSync(inventoryPath) || readFileSync(inventoryPath, 'utf8') !== output) {
  errors.push('Inventory stale: node scripts/check-docs.mjs --write');
}
if (errors.length) {
  console.error(errors.join('\n'));
  process.exitCode = 1;
} else {
  console.log(
    `${inventory.length} Markdown files inventoried; language links and local targets verified.`,
  );
}
