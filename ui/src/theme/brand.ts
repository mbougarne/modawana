// Owner brand colours → the CSS variables the components use.
// The primary fills the `majorelle*` tokens, the secondary fills `sun` / `on-sun`.
// Same rules as the Settings page in the prototype (see design-system/BRAND.md → Owner settings).
import { colors, type ColorToken, type ThemeName } from '../tokens/tokens';

export const PALETTE_VERSION = 1;

export const DEFAULT_PRIMARY = colors.light.majorelle; // #2B44C4

export const DEFAULT_SECONDARY = colors.light.sun; // #F2B33D

type BrandVars = Partial<Record<ColorToken | 'shadow-pop', string>>;

export interface DerivedBrand {
  light: BrandVars;
  dark: BrandVars;
  /** true when the primary was darkened to keep links readable */ adjusted: boolean;
}

const hex2rgb = (h: string) => {
  let x = h.replace('#', '');
  if (x.length === 3) {
    x = x
      .split('')
      .map((c) => c + c)
      .join('');
  }
  return [0, 2, 4].map((i) => parseInt(x.slice(i, i + 2), 16));
};
const rgb2hex = (c: number[]) =>
  '#' +
  c
    .map((v) =>
      Math.round(Math.max(0, Math.min(255, v)))
        .toString(16)
        .padStart(2, '0'),
    )
    .join('')
    .toUpperCase();

export const mix = (a: string, b: string, t: number) => {
  const x = hex2rgb(a);
  const y = hex2rgb(b);
  return rgb2hex(x.map((v, i) => v + (y[i] - v) * t));
};
const luminance = (h: string) => {
  const c = hex2rgb(h).map((v) => {
    v /= 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
};

/** WCAG contrast ratio between two hex colours (1–21). */
export const contrast = (a: string, b: string) => {
  const [x, y] = [luminance(a), luminance(b)].sort((m, n) => n - m);
  return (x + 0.05) / (y + 0.05);
};

export const isHex = (h: string) => /^#[0-9a-f]{6}$/i.test(h);

const until = (start: string, toward: string, step: number, ok: (c: string) => boolean) => {
  let c = start;
  for (let i = 0; i < 24 && !ok(c); i++) {
    c = mix(c, toward, step);
  }
  return c;
};
const pickOn = (bg: string, a: string, b: string) => (contrast(a, bg) >= contrast(b, bg) ? a : b);

export function deriveBrand(primary: string, secondary: string): DerivedBrand {
  if (!isHex(primary) || !isHex(secondary)) {
    throw new Error('Invalid palette');
  }
  const L = colors.light;
  const D = colors.dark;
  const p = primary.toUpperCase();
  const s = secondary.toUpperCase();
  const soft = mix(p, '#FFFFFF', 0.87);
  const pl = until(p, '#000000', 0.08, (c) =>
    [L.paper, L.surface, soft].every((bg) => contrast(c, bg) >= 4.5),
  );
  const strong = until(mix(pl, '#000000', 0.25), '#000000', 0.1, (c) => contrast(c, soft) >= 4.5);
  const softD = mix(p, D.paper, 0.72);
  const pd = until(p, '#FFFFFF', 0.08, (c) =>
    [D.paper, D.surface, softD].every((bg) => contrast(c, bg) >= 4.5),
  );
  const strongD = until(mix(pd, '#FFFFFF', 0.35), '#FFFFFF', 0.1, (c) => contrast(c, softD) >= 4.5);
  const sd = mix(s, '#FFFFFF', 0.1);
  const out: DerivedBrand = {
    adjusted: pl !== p || pd !== p,
    light: {
      majorelle: pl,
      'majorelle-strong': strong,
      'majorelle-soft': soft,
      'on-majorelle': pickOn(pl, '#FFFFFF', L.ink),
      sun: s,
      'on-sun': pickOn(s, L.ink, '#FFFFFF'),
      'shadow-pop': `4px 4px 0 0 ${L.ink}`,
    },
    dark: {
      majorelle: pd,
      'majorelle-strong': strongD,
      'majorelle-soft': softD,
      'on-majorelle': pickOn(pd, D.paper, '#FFFFFF'),
      sun: sd,
      'on-sun': pickOn(sd, D.paper, '#FFFFFF'),
      'shadow-pop': `4px 4px 0 0 ${pd}`,
    },
  };
  // The default pair keeps the hand-tuned values from tokens.json.
  if (p === DEFAULT_PRIMARY.toUpperCase()) {
    out.adjusted = false;
    (['light', 'dark'] as ThemeName[]).forEach((th) =>
      (['majorelle', 'majorelle-strong', 'majorelle-soft', 'on-majorelle'] as const).forEach(
        (k) => {
          out[th][k] = colors[th][k];
        },
      ),
    );
    out.dark['shadow-pop'] = `4px 4px 0 0 ${colors.dark.majorelle}`;
  }
  if (s === DEFAULT_SECONDARY.toUpperCase()) {
    (['light', 'dark'] as ThemeName[]).forEach((th) => {
      out[th].sun = colors[th].sun;
      out[th]['on-sun'] = colors[th]['on-sun'];
    });
  }
  return out;
}

/** CSS text that overrides the brand tokens for both themes. Render it server-side in <head> to avoid a flash. */
export function brandCss(primary: string, secondary: string): string {
  if (!isHex(primary) || !isHex(secondary)) {
    return '';
  }
  if (
    primary.toUpperCase() === DEFAULT_PRIMARY.toUpperCase() &&
    secondary.toUpperCase() === DEFAULT_SECONDARY.toUpperCase()
  ) {
    return '';
  }
  const d = deriveBrand(primary, secondary);
  const block = (v: BrandVars) =>
    Object.entries(v)
      .map(([k, x]) => `--${k}:${x};`)
      .join('');
  return `:root,[data-theme="light"]{${block(d.light)}}[data-theme="dark"]{${block(d.dark)}}@media (prefers-color-scheme: dark){:root:not([data-theme="light"]){${block(d.dark)}}}`;
}

/** Contrast checks shown on the Settings page. */
export function brandChecks(primary: string, secondary: string) {
  const d = deriveBrand(primary, secondary);
  return (['light', 'dark'] as ThemeName[]).flatMap((th) => {
    const c = d[th] as Record<string, string>;
    const paper = colors[th].paper;
    return [
      { theme: th, check: 'links', ratio: contrast(c.majorelle, paper) },
      { theme: th, check: 'surface', ratio: contrast(c.majorelle, colors[th].surface) },
      { theme: th, check: 'focus', ratio: contrast(c.majorelle, c['majorelle-soft']) },
      { theme: th, check: 'button', ratio: contrast(c['on-majorelle'], c.majorelle) },
      { theme: th, check: 'soft', ratio: contrast(c['majorelle-strong'], c['majorelle-soft']) },
      { theme: th, check: 'secondary', ratio: contrast(c['on-sun'], c.sun) },
    ].map((r) => ({ ...r, pass: r.ratio >= 4.5 }));
  });
}
