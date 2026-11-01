#!/usr/bin/env node
// Builds design-system/tokens.css from design-system/tokens.json.
// Usage: node design-system/scripts/build-tokens.mjs        (write tokens.css)
//        node design-system/scripts/build-tokens.mjs --check (exit 1 if tokens.css is stale)
// No dependencies. tokens.json is the source of truth; never edit tokens.css by hand.
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const tokens = JSON.parse(readFileSync(join(root, 'tokens.json'), 'utf8'));
const themes = tokens.color.themes.map((t) => t.id);
const first = themes[0];

const colorValue = (token, theme) => {
  const v =
    typeof token.value === 'string' ? token.value : (token.value[theme] ?? token.value[first]);
  return v.replace(/^\{(.+)\}$/, 'var(--$1)');
};
const perTheme = (theme) =>
  [
    ...tokens.color.tokens.map((t) => `  --${t.name}: ${colorValue(t, theme)};`),
    ...(tokens.shadow?.tokens ?? []).map((t) => `  --${t.name}: ${colorValue(t, theme)};`),
  ].join('\n');

const plainFamilies = ['spacing', 'radius', 'size'];
const plain = plainFamilies.flatMap((f) =>
  (tokens[f]?.tokens ?? []).map((t) => `  --${t.name}: ${t.value};`),
);
const presetVariables = Object.entries(tokens.brandPresets ?? {}).flatMap(([role, presets]) =>
  presets.map((p) => `  --preset-${role}-${p.id}: ${p.value};`),
);
const presetClasses = Object.entries(tokens.brandPresets ?? {}).flatMap(([role, presets]) =>
  presets.map(
    (p) => `.brand-preset-${role}-${p.id} { background: var(--preset-${role}-${p.id}); }`,
  ),
);
const fonts = Object.entries(tokens.type.families).map(([k, v]) => `  --font-${k}: ${v};`);

const px = (v) => (typeof v === 'number' ? `${v}px` : v);
const typeClasses = tokens.type.groups.flatMap((g) =>
  g.styles.map((s) => {
    const fam = s.family ?? g.family;
    const decl = [
      `font-family: var(--font-${fam})`,
      `font-size: ${px(s.fontSize)}`,
      s.lineHeight && `line-height: ${s.lineHeight}`,
      s.fontWeight && `font-weight: ${s.fontWeight}`,
      s.fontStyle && `font-style: ${s.fontStyle}`,
      s.letterSpacing && `letter-spacing: ${s.letterSpacing}`,
    ]
      .filter(Boolean)
      .join('; ');
    return `.ds-${s.name} { ${decl}; }`;
  }),
);

const dark = themes.find((t) => t === 'dark');
const css = `/* ${tokens.name} — generated from tokens.json by scripts/build-tokens.mjs. Do not edit by hand. */

:root, [data-theme="${first}"] {
${perTheme(first)}
  color-scheme: ${first};
}
${themes
  .slice(1)
  .map((t) => `[data-theme="${t}"] {\n${perTheme(t)}\n  color-scheme: ${t};\n}`)
  .join('\n')}
${dark ? `@media (prefers-color-scheme: dark) {\n  :root:not([data-theme="${first}"]) {\n${perTheme(dark).replace(/^/gm, '  ')}\n    color-scheme: dark;\n  }\n}\n` : ''}
:root {
${[...plain, ...fonts, ...presetVariables].join('\n')}
}

/* Type styles: one class per tokens.json type style. */
${presetClasses.join('\n')}
${typeClasses.join('\n')}
`;

const out = join(root, 'tokens.css');
if (process.argv.includes('--check')) {
  const current = readFileSync(out, 'utf8');
  if (current !== css) {
    console.error('tokens.css is out of date. Run: node design-system/scripts/build-tokens.mjs');
    process.exit(1);
  }
  console.log('tokens.css is up to date.');
} else {
  writeFileSync(out, css);
  console.log(`Wrote ${out}`);
}
