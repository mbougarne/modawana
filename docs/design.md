# Design and shared UI

[العربية](design.ar.md) · [Documentation](README.md)

The interface uses a personal home header, compact writing lists, readable content,
and a private editing workspace. Branding comes from saved settings, not a hardcoded
identity. Keep one consistent logo/navigation control; the profile portrait is a
separate role. Layouts support English, Arabic RTL, French, light and dark themes.

## Source layout

`ui/src` is application source imported through `@ui`, not a separate application.
It contains shared components, fonts, models, providers, and labels. `design-system/`
contains the canonical token JSON, generator, and component stylesheet. These files
are required to reproduce builds and must remain tracked. Design documentation lives here.

| Source | Use |
| --- | --- |
| `design-system/tokens.json` | Colours, typography, spacing, radii and font families |
| `ui/src/styles/index.css` | Shared style entry point |
| `ui/src/theme` | ThemeProvider and CSP-aware BrandStyle |
| `ui/src/i18n` | I18nProvider, typed labels, plural rules and controlled fallback |
| `ui/src/fonts` | Self-hosted WOFF2 fonts and original OFL licenses |
| `ui/src/components`, `ui/src/editor` | Reusable reading, editing and form controls |

Edit tokens in JSON, then regenerate from the repository root:

```sh
node design-system/scripts/build-tokens.mjs
node ui/scripts/build-tokens.mjs
node scripts/build-label-registry.mjs
node scripts/build-palette-vectors.mjs
```

The label command is needed after label-default changes; palette vectors after
palette-algorithm changes. `--check` verifies generated files. Keep
`design-system/components.css` and `ui/src/styles/components.css` identical.

## Colour and type

Use semantic CSS variables: `paper`, `surface`, `ink`, `ink-muted`, `majorelle`,
`sun`, and `rose`. The default primary is Majorelle blue and the secondary is yellow;
owner settings derive accessible light/dark shades from two chosen colours.
Use the shared derivation algorithm rather than arbitrary overrides. Branding styles
must preserve the response nonce required by the Content Security Policy.

Bricolage Grotesque is the display face, IBM Plex Sans the text face, IBM Plex Mono
code/metadata, and IBM Plex Sans Arabic Arabic text. Preserve the font notices.
Keep reading width bounded, body-text contrast at least 4.5:1, visible focus,
44px touch targets, and reduced-motion support.

## Languages and interaction

Every interface label uses `t()` with EN/AR/FR entries. Use logical CSS properties,
page `lang`/`dir`, and isolated LTR code/identifiers. Do not apply letter spacing to
Arabic. Profile and interface labels allow documented fallback; article, lesson,
documentation and quiz bodies require explicit translation availability.

Forms connect visible labels, errors and help to inputs. Keep saving, failure,
conflict, empty and loading states explicit. The editor provides writing, preview,
media, quiz and details controls. Saved and published state must remain distinct.

Locked lessons receive only public teasers. Quiz components never grade locally or
receive answer keys in reader payloads. See [UI contracts](ui-contracts.md),
[Markdown](markdown-reader.md), and [learning API](api/learning.md).
