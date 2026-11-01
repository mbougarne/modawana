import assert from 'node:assert/strict';
import { test } from 'node:test';
import { resolve } from 'node:path';
import { ESLint } from '../frontend/node_modules/eslint/lib/api.js';

const root = resolve(import.meta.dirname, '..');
const eslint = new ESLint({
  cwd: root,
  overrideConfigFile: resolve(root, 'frontend/eslint.config.mjs'),
});

test('configured import rules still reject misplaced and duplicate imports', async () => {
  const [result] = await eslint.lintText(
    "const value = 1;\nimport { useState } from 'react';\nimport { useEffect } from 'react';\nconsole.log(value, useState, useEffect);\n",
    { filePath: 'frontend/src/lint-regression.ts' },
  );
  const rules = new Set(result.messages.map((message) => message.ruleId));
  assert.ok(rules.has('import-x/first'));
  assert.ok(rules.has('import-x/no-duplicates'));
});

test('configured lint globs include sources and exclude generated output', async () => {
  assert.equal(await eslint.isPathIgnored('frontend/src/lint-regression.ts'), false);
  assert.equal(await eslint.isPathIgnored('frontend/dist/lint-regression.js'), true);
  assert.equal(await eslint.isPathIgnored('ui/src/tokens/lint-regression.ts'), true);
  const [result] = await eslint.lintText(
    "import { useState, useEffect } from 'react';\n\nconsole.log(useState, useEffect);\n",
    { filePath: 'frontend/src/lint-regression.ts' },
  );
  assert.equal(result.errorCount, 0, JSON.stringify(result.messages));
});
