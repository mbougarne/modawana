import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import vm from 'node:vm';

const require = createRequire(new URL('../frontend/package.json', import.meta.url));
const ts = require('typescript');
const source = readFileSync(new URL('../ui/src/i18n/labels.ts', import.meta.url), 'utf8');
const js = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS },
}).outputText;
const context = { exports: {} };
vm.runInNewContext(js, context);
const registry = Object.fromEntries(
  Object.entries(context.exports.DEFAULT_LABELS).map(([key, values]) => [
    key,
    {
      section: key.split('.')[0],
      description: `Interface text: ${key}`,
      placeholders: [
        ...new Set(
          JSON.stringify(values.en)
            .matchAll(/\{([a-zA-Z][a-zA-Z0-9_]*)\}/g)
            .map((m) => m[1]),
        ),
      ].sort(),
      values,
    },
  ]),
);
const path = new URL('../backend/internal/siteconfig/labels.json', import.meta.url);
const body = JSON.stringify(registry, null, 2) + '\n';
if (process.argv.includes('--check')) {
  if (readFileSync(path, 'utf8') !== body) {
    throw Error('Label registry is stale');
  }
} else {
  writeFileSync(path, body);
}
console.log('Label registry synchronized.');
