import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import stylistic from '@stylistic/eslint-plugin';
import importFirst from 'eslint-plugin-import-x/rules/first';
import importNoDuplicates from 'eslint-plugin-import-x/rules/no-duplicates';
import globals from 'globals';

// Load only the rules we enable: the full plugin eagerly imports makeRe,
// which minimatch 11 removed, through its unused no-internal-modules rule.
const importX = { rules: { first: importFirst, 'no-duplicates': importNoDuplicates } };

export default tseslint.config(
  {
    ignores: [
      '**/node_modules/**',
      '**/dist/**',
      '**/.test-output/**',
      '**/test-results/**',
      'build/**',
      'internal/**',
      'design-system/prototype/**',
      'design-system/previews/**',
      'ui/src/tokens/**',
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{js,cjs,mjs,ts,tsx}'],
    languageOptions: { globals: { ...globals.browser, ...globals.node } },
    plugins: { '@stylistic': stylistic, 'import-x': importX },
    rules: {
      curly: ['error', 'all'],
      'one-var': ['error', 'never'],
      'import-x/first': 'error',
      'import-x/no-duplicates': 'error',
      '@stylistic/padding-line-between-statements': [
        'error',
        { blankLine: 'always', prev: 'import', next: '*' },
        { blankLine: 'any', prev: 'import', next: 'import' },
        { blankLine: 'always', prev: '*', next: 'export' },
      ],
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
          ignoreRestSiblings: true,
        },
      ],
    },
  },
  {
    files: ['**/*.{js,cjs,mjs}'],
    rules: { '@typescript-eslint/no-require-imports': 'off' },
  },
);
