// Builds the first-party example plugin to WebAssembly (WASI). The binary is
// never committed: the integration tests compile it on demand, and this
// script produces the same artifact for manual installs.
//
//   node scripts/build-example-plugin.mjs [output.wasm]
import { spawn } from 'node:child_process';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const output = resolve(
  root,
  process.argv[2] || 'backend/internal/plugin/testdata/example/plugin.wasm',
);
const child = spawn('go', ['build', '-o', output, './internal/plugin/testdata/example'], {
  cwd: resolve(root, 'backend'),
  env: { ...process.env, GOOS: 'wasip1', GOARCH: 'wasm', CGO_ENABLED: '0' },
  stdio: 'inherit',
});
child.on('exit', (code) => {
  if (code === 0) {
    console.log('Built ' + output);
  }
  process.exit(code ?? 1);
});
