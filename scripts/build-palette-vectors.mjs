import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import vm from 'node:vm';

const require = createRequire(new URL('../frontend/package.json', import.meta.url));
const ts = require('typescript');
function compile(file, dep) {
  const context = { exports: {}, require: () => dep };
  vm.runInNewContext(
    ts.transpileModule(readFileSync(new URL(file, import.meta.url), 'utf8'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS },
    }).outputText,
    context,
  );
  return context.exports;
}
const tokens = compile('../ui/src/tokens/tokens.ts');
const brand = compile('../ui/src/theme/brand.ts', tokens);
const pairs = [
  ['#2B44C4', '#F2B33D'],
  ['#FFFFFF', '#FFFFFF'],
  ['#000000', '#000000'],
  ['#00FF00', '#FF00FF'],
  ['#004488', '#F2B33D'],
  ['#FF0000', '#777777'],
  ['#123456', '#FFAA00'],
];
const vectors = pairs.map(([primary, secondary]) => ({
  primary,
  secondary,
  version: brand.PALETTE_VERSION,
  ...brand.deriveBrand(primary, secondary),
  checks: brand.brandChecks(primary, secondary),
}));
const files = [
  ['../backend/internal/siteconfig/palette-defaults.json', tokens.colors],
  ['../backend/internal/siteconfig/testdata/palette-vectors.json', vectors],
];
mkdirSync(new URL('../backend/internal/siteconfig/testdata/', import.meta.url), {
  recursive: true,
});
for (const [file, v] of files) {
  const p = new URL(file, import.meta.url);
  const body = JSON.stringify(v, null, 2) + '\n';
  if (process.argv.includes('--check')) {
    if (readFileSync(p, 'utf8') !== body) {
      throw Error('Palette data is stale');
    }
  } else {
    writeFileSync(p, body);
  }
}
console.log('Palette defaults and shared vectors synchronized.');
