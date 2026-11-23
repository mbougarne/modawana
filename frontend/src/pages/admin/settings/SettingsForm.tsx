import { BrandColourField } from '../../../../../ui/src/components/BrandColourField';
import { useId, useState } from 'react';
import { Logo, deriveBrand, brandChecks, useI18n, Label } from '@ui';
import { ProfilePreview } from '../profile/ProfileForm';
import { usePublicConfiguration } from '../../../features/configuration/PublicConfiguration';
import { siteBrand } from './siteBrand';
import type { PublicThemeID, SiteSettings } from '../../../api/configuration';
import { MediaInput, Section, TextField } from '../../../features/configuration/controls';
import { PublicThemeSettings } from './PublicThemeSettings';
import { MenuSettings } from './MenuSettings';
import { useOwnerAppearanceContext } from '../../../features/owner/OwnerAppearance';
import { PublicLanguageSettings } from './PublicLanguageSettings';

export function SettingsForm({
  value: s,
  savedTheme,
  onCSRF,
  onChange,
  csrf,
}: {
  value: SiteSettings;
  savedTheme?: PublicThemeID;
  onCSRF: (csrf: string) => void;
  onChange: (s: SiteSettings) => void;
  csrf: string;
}) {
  const { t, locale } = useI18n();
  const { theme } = useOwnerAppearanceContext();
  const { data } = usePublicConfiguration();
  const [previewTheme, setPreviewTheme] = useState<'light' | 'dark'>('light');
  const change = <K extends keyof SiteSettings>(k: K, v: SiteSettings[K]) =>
    onChange({ ...s, [k]: v });
  const id = 'preview-' + useId().replace(/[^a-zA-Z0-9]/g, '');
  let derived;
  let checks;
  try {
    derived = deriveBrand(s.primary, s.secondary);
    checks = brandChecks(s.primary, s.secondary);
  } catch {
    /* Invalid colours are explained by preview validation. */
  }
  return (
    <div className="config-editor-layout config-settings-editor">
      <div className="config-edit-controls">
        <PublicLanguageSettings
          value={s.public_locales}
          onChange={(locales) => change('public_locales', locales)}
        />
        <Section name="config.identity">
          <label className="config-field">
            <Label name="config.logoMode" />
            <select
              data-config-path="logo_mode"
              value={s.logo_mode}
              onChange={(e) => change('logo_mode', e.target.value as SiteSettings['logo_mode'])}
            >
              {(['wordmark', 'monogram', 'image'] as const).map((mode) => (
                <option key={mode} value={mode}>
                  {t('config.' + mode)}
                </option>
              ))}
            </select>
          </label>
          <TextField
            name="config.wordmark"
            path="wordmark"
            value={s.wordmark}
            max={80}
            onChange={(v) => change('wordmark', v)}
          />
          <TextField
            name="config.monogram"
            path="monogram"
            value={s.monogram}
            max={64}
            onChange={(v) => change('monogram', v)}
          />
          <MediaInput
            name="config.logo"
            purpose="logo"
            value={s.logo_media_id}
            csrf={csrf}
            onChange={(v) => change('logo_media_id', v)}
          />
          <MediaInput
            name="config.favicon"
            purpose="favicon"
            value={s.favicon_media_id}
            csrf={csrf}
            onChange={(v) => change('favicon_media_id', v)}
          />
        </Section>
        {theme === 'classic' && (
          <PublicThemeSettings
            value={s.public_theme}
            saved={savedTheme}
            onCSRF={onCSRF}
            onChange={(theme) => change('public_theme', theme)}
          />
        )}
        <Section name="config.colours">
          {(['primary', 'secondary'] as const).map((k) => (
            <BrandColourField key={k} role={k} value={s[k]} onChange={(v) => change(k, v)} />
          ))}
          <p>
            <Label name="config.adjusted" />
          </p>
          <ul id="contrast" className="config-checks">
            {checks?.map((c) => (
              <li key={c.theme + c.check}>
                {t('config.' + c.theme)} · {t('contrast.' + c.check)}:{' '}
                <bdi>{c.ratio.toFixed(2)}:1</bdi> — {t(c.pass ? 'config.pass' : 'config.fail')}
              </li>
            ))}
          </ul>
        </Section>
        {theme === 'classic' && <MenuSettings value={s} onChange={onChange} />}
      </div>
      <aside
        className="config-panel config-live-preview"
        aria-label={t('config.livePreview')}
        tabIndex={0}
      >
        <div className="config-preview-heading">
          <h2>{t('config.livePreview')}</h2>
          <div className="config-preview-actions">
            {(['light', 'dark'] as const).map((theme) => (
              <button
                key={theme}
                type="button"
                className="vb-btn vb-btn-secondary vb-btn-sm"
                aria-pressed={previewTheme === theme}
                onClick={() => setPreviewTheme(theme)}
              >
                {t('config.' + theme)}
              </button>
            ))}
          </div>
        </div>
        {derived && (
          <>
            <style
              nonce={document.querySelector<HTMLMetaElement>('meta[name="csp-nonce"]')?.content}
            >
              {(['light', 'dark'] as const)
                .map(
                  (theme) =>
                    `#${id}-${theme}{${Object.entries(derived[theme])
                      .map(([k, v]) => `--${k}:${v};`)
                      .join('')}}`,
                )
                .join('')}
            </style>
            <div className="config-preview-pair">
              {([previewTheme] as const).map((theme) => (
                <div
                  key={theme}
                  id={id + '-' + theme}
                  className="config-brand-preview"
                  data-theme={theme}
                >
                  <Logo brand={siteBrand(s)} href={'/' + locale} />
                  {data?.profile && (
                    <ProfilePreview value={data.profile} locale={locale} monogram={s.monogram} />
                  )}
                  <button className="vb-btn vb-btn-primary" type="button">
                    {t('config.sampleButton')}
                  </button>
                  <p>
                    <a href="#contrast">{t('config.sampleLink')}</a>
                  </p>
                  <span className="vb-sticker">{t('config.sampleTag')}</span>
                </div>
              ))}
            </div>
          </>
        )}
      </aside>
    </div>
  );
}
