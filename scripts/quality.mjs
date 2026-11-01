import { spawnSync } from 'node:child_process';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const mode = process.argv[2];
const fix = process.argv.includes('--fix');
const binaries = resolve(root, 'frontend/node_modules');

function run(command, args) {
  const result = spawnSync(command, args, { cwd: root, stdio: 'inherit' });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}

if (mode === 'lint') {
  run(process.execPath, [
    resolve(binaries, 'eslint/bin/eslint.js'),
    '--config',
    'frontend/eslint.config.mjs',
    '.',
    '--max-warnings=0',
    ...(fix ? ['--fix'] : []),
  ]);
} else if (mode === 'format') {
  run(process.execPath, [
    resolve(binaries, 'prettier/bin/prettier.cjs'),
    fix ? '--write' : '--check',
    '.prettierrc.json',
    'frontend/**/*.{js,cjs,mjs,ts,tsx,css,json}',
    'ui/**/*.{js,mjs,ts,tsx,css}',
    'scripts/**/*.{js,cjs,mjs}',
    'design-system/scripts/*.mjs',
    'design-system/components.css',
    '.github/workflows/*.yml',
  ]);
  if (fix) {
    run('gofmt', ['-w', 'backend']);
  } else {
    const result = spawnSync('gofmt', ['-l', 'backend'], { cwd: root, encoding: 'utf8' });
    if (result.error || result.status !== 0 || result.stdout.trim()) {
      console.error(result.error?.message ?? result.stderr ?? '');
      console.error(result.stdout);
      process.exit(1);
    }
  }
} else {
  throw new Error('Usage: node scripts/quality.mjs lint|format [--fix]');
}
