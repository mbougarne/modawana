import { createRequire } from 'node:module';

const require = createRequire(new URL('../frontend/package.json', import.meta.url));
const { DEFAULT_LABELS } = require('./.test-output/ui/src/i18n/labels.js');
const { resolveLabel } = require('./.test-output/ui/src/i18n/resolve.js');

export function smokeTranslator(locale) {
  return (key, vars) => {
    if (!Object.hasOwn(DEFAULT_LABELS, key)) {
      throw new Error(`Unknown smoke label: ${key}`);
    }
    const { text } = resolveLabel(key, locale, undefined, vars);
    if (/\{\w+\}/.test(text)) {
      throw new Error(`Missing smoke label variables: ${key} (${locale})`);
    }
    return text;
  };
}
