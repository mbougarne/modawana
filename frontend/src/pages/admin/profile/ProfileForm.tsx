import { AboutSelect } from './AboutSelect';
import { usePublicConfiguration } from '../../../features/configuration/PublicConfiguration';
import { useState, useId, type ReactNode } from 'react';
import {
  Button,
  Portrait,
  renderHeadline,
  renderBio,
  Label,
  useI18n,
  Icon,
  type IconName,
} from '@ui';
import type { ProfileSettings, Localized, ProfileLink } from '../../../api/configuration';
import type { Locale } from '../../../api/content';
import {
  MediaInput,
  Section,
  TextField,
  LocalizedField,
  Check,
  languages,
} from '../../../features/configuration/controls';
import { safeLink } from '../../../features/markdown/MarkdownReader';
import { iconNames } from '../settings/siteBrand';

const id = () => crypto.randomUUID();
const empty = (): Localized => ({ en: '', fr: '', ar: '' });
function Move({
  index,
  total,
  onMove,
  onRemove,
}: {
  index: number;
  total: number;
  onMove: (to: number) => void;
  onRemove: () => void;
}) {
  return (
    <div className="config-actions">
      <Button disabled={!index} onClick={() => onMove(index - 1)}>
        <Label name="config.up" />
      </Button>
      <Button disabled={index === total - 1} onClick={() => onMove(index + 1)}>
        <Label name="config.down" />
      </Button>
      <Button onClick={onRemove}>
        <Label name="config.remove" />
      </Button>
    </div>
  );
}
function reorder<T>(a: T[], from: number, to: number) {
  const out = [...a];
  const [item] = out.splice(from, 1);
  out.splice(to, 0, item);
  return out;
}

export function ProfilePreview({
  value: p,
  locale,
  monogram = 'B',
}: {
  value: ProfileSettings;
  locale: Locale;
  monogram?: string;
}) {
  const { t } = useI18n();
  const uid = 'banner-' + useId().replace(/[^a-zA-Z0-9]/g, '');
  const selected = (v: Localized) => v?.[locale] || v?.en || '';
  function localized(v: Localized, render: (s: string) => ReactNode = (s) => s) {
    const text = selected(v);
    if (!text) {
      return null;
    }
    const actual = v?.[locale] ? locale : 'en';
    return (
      <span lang={actual} dir={actual === 'ar' ? 'rtl' : 'ltr'}>
        {render(text)}
        {actual !== locale && <small className="config-fallback"> ({t('config.fallback')})</small>}
      </span>
    );
  }
  const x = Math.max(0, Math.min(100, p.banner_x));
  const y = Math.max(0, Math.min(100, p.banner_y));
  return (
    <section className="config-profile-preview" lang={locale} dir={locale === 'ar' ? 'rtl' : 'ltr'}>
      {p.banner_enabled && p.banner_media_id && (
        <>
          <style
            nonce={document.querySelector<HTMLMetaElement>('meta[name="csp-nonce"]')?.content}
          >{`#${uid}{object-position:${x}% ${y}%;}`}</style>
          <img
            id={uid}
            className="config-banner"
            src={'/media/' + p.banner_media_id}
            alt={selected(p.banner_alt)}
          />
        </>
      )}
      <div className="config-hero">
        <div>
          {selected(p.name) && <p className="vb-meta">{localized(p.name)}</p>}
          {selected(p.headline) && <h2>{localized(p.headline, renderHeadline)}</h2>}
          {selected(p.bio) && <p className="vb-lead">{localized(p.bio, renderBio)}</p>}
        </div>
        <Portrait
          src={p.portrait_media_id ? '/media/' + p.portrait_media_id : undefined}
          alt={selected(p.portrait_alt)}
          monogram={monogram}
          sticker={selected(p.sticker)}
        />
      </div>
      <div className="config-actions">
        {[...(p.main_action ? [p.main_action] : []), ...p.links]
          .filter((l) => l.visible && selected(l.label))
          .map((l) => (
            <a
              className={
                'vb-btn ' +
                (l.id === p.main_action?.id
                  ? 'vb-btn-primary'
                  : l.icon === 'rss'
                    ? 'vb-btn-quiet'
                    : 'vb-btn-secondary')
              }
              key={l.id}
              href={safeLink(l.url)}
            >
              {l.icon && iconNames.includes(l.icon) && <Icon name={l.icon as IconName} />}{' '}
              {localized(l.label)}
            </a>
          ))}
      </div>
      {p.show_now && p.now.some((r) => selected(r.text)) && (
        <section className="vb-now">
          <h3>
            <Label name="home.now" />
          </h3>
          {p.updated_month && (
            <time dateTime={p.updated_month}>
              {new Intl.DateTimeFormat(locale === 'ar' ? 'ar-MA' : locale, {
                year: 'numeric',
                month: 'long',
                timeZone: 'UTC',
              }).format(new Date(p.updated_month + '-01T00:00:00Z'))}
            </time>
          )}
          <ul>
            {p.now
              .filter((r) => selected(r.text))
              .map((r) => (
                <li key={r.id}>
                  <b>{localized(r.label)}</b> {localized(r.text)}
                </li>
              ))}
          </ul>
        </section>
      )}
      {p.experience.some((e) => selected(e.role) || selected(e.organization)) && (
        <section>
          <h3>
            <Label name="config.experience" />
          </h3>
          <ol>
            {p.experience.map((e) => (
              <li key={e.id}>
                {localized(e.role)} · {localized(e.organization)}{' '}
                <bdi>
                  {e.start} – {e.current ? t('config.current') : e.end}
                </bdi>
                {selected(e.description) && <p>{localized(e.description)}</p>}
                {e.url && (
                  <a href={safeLink(e.url)}>
                    <Label name="config.website" />
                  </a>
                )}
              </li>
            ))}
          </ol>
        </section>
      )}
      {p.public_email && (
        <a href={safeLink('mailto:' + p.public_email)}>
          <bdi>{p.public_email}</bdi>
        </a>
      )}
      {selected(p.location) && <p>{localized(p.location)}</p>}
      {selected(p.footer) && <p>{localized(p.footer)}</p>}
      {selected(p.copyright) && <p>© {localized(p.copyright)}</p>}
    </section>
  );
}

export function ProfileForm({
  value: p,
  onChange,
  csrf,
}: {
  value: ProfileSettings;
  onChange: (p: ProfileSettings) => void;
  csrf: string;
}) {
  const { t } = useI18n();
  const { data } = usePublicConfiguration();
  const [previewLocale, setPreviewLocale] = useState<Locale>('en');
  const change = <K extends keyof ProfileSettings>(k: K, v: ProfileSettings[K]) =>
    onChange({ ...p, [k]: v });
  const linkFields = (l: ProfileLink, set: (v: ProfileLink) => void, path: string) => (
    <>
      <LocalizedField
        name="config.linkLabel"
        path={path + '.label'}
        value={l.label}
        max={80}
        onChange={(v) => set({ ...l, label: v })}
      />
      <TextField
        name="config.url"
        path={path + '.url'}
        value={l.url}
        max={2048}
        onChange={(v) => set({ ...l, url: v })}
      />
      <label className="config-field">
        <Label name="config.icon" />
        <select value={l.icon} onChange={(e) => set({ ...l, icon: e.target.value })}>
          <option value="">{t('config.none')}</option>
          {iconNames.map((i) => (
            <option key={i} value={i}>
              {t('icon.' + i)}
            </option>
          ))}
        </select>
      </label>
      <Check name="config.visible" value={l.visible} onChange={(v) => set({ ...l, visible: v })} />
    </>
  );
  return (
    <div className="config-editor-layout">
      <div className="config-edit-controls">
        <p className="vb-help">{t('validation.profileHelp')}</p>
        <Section name="config.profileIdentity">
          {(['name', 'headline', 'bio', 'sticker', 'portrait_alt'] as const).map((k) => (
            <LocalizedField
              key={k}
              name={'config.' + k}
              path={k}
              value={p[k]}
              max={k === 'bio' ? 320 : k === 'name' ? 100 : k === 'sticker' ? 40 : 200}
              multiline={k === 'headline' || k === 'bio'}
              onChange={(v) => change(k, v)}
            />
          ))}
          <MediaInput
            name="config.portrait"
            purpose="portrait"
            value={p.portrait_media_id}
            csrf={csrf}
            onChange={(v) => change('portrait_media_id', v)}
          />
          <p className="vb-help">
            <Label name="config.portraitHelp" />
          </p>
        </Section>
        <Section name="config.banner">
          <Check
            name="config.bannerEnabled"
            value={p.banner_enabled}
            onChange={(v) => change('banner_enabled', v)}
          />
          <MediaInput
            name="config.banner"
            purpose="banner"
            value={p.banner_media_id}
            csrf={csrf}
            onChange={(v) => change('banner_media_id', v)}
          />
          <LocalizedField
            name="config.banner_alt"
            path="banner_alt"
            value={p.banner_alt}
            onChange={(v) => change('banner_alt', v)}
          />
          {(['banner_x', 'banner_y'] as const).map((k) => (
            <label key={k} className="config-field">
              <Label name={'config.' + k} />
              <input
                type="range"
                data-config-path={k}
                min={0}
                max={100}
                value={p[k]}
                onChange={(e) => change(k, Number(e.target.value))}
              />
            </label>
          ))}
        </Section>
        <Section name="config.now">
          <TextField
            name="config.updatedMonth"
            path="updated_month"
            type="month"
            value={p.updated_month}
            onChange={(v) => change('updated_month', v)}
          />
          {p.now.map((r, i) => (
            <fieldset className="config-row" key={r.id}>
              <legend>{t('config.row', { n: i + 1 })}</legend>
              <LocalizedField
                name="config.rowLabel"
                path={'now.' + i + '.label'}
                value={r.label}
                max={80}
                onChange={(v) =>
                  change(
                    'now',
                    p.now.map((x) => (x.id === r.id ? { ...x, label: v } : x)),
                  )
                }
              />
              <LocalizedField
                name="config.rowText"
                path={'now.' + i + '.text'}
                value={r.text}
                max={160}
                onChange={(v) =>
                  change(
                    'now',
                    p.now.map((x) => (x.id === r.id ? { ...x, text: v } : x)),
                  )
                }
              />
              <Move
                index={i}
                total={p.now.length}
                onMove={(to) => change('now', reorder(p.now, i, to))}
                onRemove={() =>
                  change(
                    'now',
                    p.now.filter((x) => x.id !== r.id),
                  )
                }
              />
            </fieldset>
          ))}
          <Button
            disabled={p.now.length >= 3}
            onClick={() => change('now', [...p.now, { id: id(), label: empty(), text: empty() }])}
          >
            <Label name="config.addRow" />
          </Button>
        </Section>
        <Section name="config.links">
          <p className="vb-help">
            <Label name="config.mainActionHelp" />
          </p>
          <Check
            name="config.mainAction"
            value={!!p.main_action}
            onChange={(v) =>
              change(
                'main_action',
                v ? { id: id(), label: empty(), url: '', icon: '', visible: true } : null,
              )
            }
          />
          {p.main_action && (
            <fieldset className="config-row">
              <legend>
                <Label name="config.mainAction" />
              </legend>
              {linkFields(p.main_action, (v) => change('main_action', v), 'main_action')}
            </fieldset>
          )}
          {p.links.map((l, i) => (
            <fieldset key={l.id} className="config-row config-link-row">
              <legend>{t('config.row', { n: i + 1 })}</legend>
              {linkFields(
                l,
                (v) =>
                  change(
                    'links',
                    p.links.map((x) => (x.id === l.id ? v : x)),
                  ),
                'links.' + i,
              )}
              <div className="config-link-actions">
                <Button
                  onClick={() =>
                    onChange({
                      ...p,
                      main_action: l,
                      links: p.links.flatMap((item) =>
                        item.id === l.id ? (p.main_action ? [p.main_action] : []) : [item],
                      ),
                    })
                  }
                >
                  <Label name="config.useMainAction" />
                </Button>
                <Move
                  index={i}
                  total={p.links.length}
                  onMove={(to) => change('links', reorder(p.links, i, to))}
                  onRemove={() =>
                    change(
                      'links',
                      p.links.filter((x) => x.id !== l.id),
                    )
                  }
                />
              </div>
            </fieldset>
          ))}
          <Button
            disabled={p.links.length >= 10}
            onClick={() =>
              change('links', [
                ...p.links,
                { id: id(), label: empty(), url: '', icon: '', visible: true },
              ])
            }
          >
            <Label name="config.addLink" />
          </Button>
        </Section>
        <Section name="config.experience">
          {p.experience.map((e, i) => {
            const set = (v: typeof e) =>
              change(
                'experience',
                p.experience.map((x) => (x.id === e.id ? v : x)),
              );
            return (
              <fieldset key={e.id} className="config-row">
                <legend>{t('config.row', { n: i + 1 })}</legend>
                {(['role', 'organization', 'description'] as const).map((k) => (
                  <LocalizedField
                    key={k}
                    name={'config.' + k}
                    path={'experience.' + i + '.' + k}
                    value={e[k]}
                    max={k === 'description' ? 500 : 120}
                    onChange={(v) => set({ ...e, [k]: v })}
                  />
                ))}
                <TextField
                  name="config.start"
                  path={'experience.' + i + '.start'}
                  type="month"
                  value={e.start}
                  onChange={(v) => set({ ...e, start: v })}
                />
                <Check
                  name="config.current"
                  value={e.current}
                  onChange={(v) => set({ ...e, current: v, end: v ? '' : e.end })}
                />
                {!e.current && (
                  <TextField
                    name="config.end"
                    path={'experience.' + i + '.end'}
                    type="month"
                    value={e.end}
                    onChange={(v) => set({ ...e, end: v })}
                  />
                )}
                <TextField
                  name="config.experienceUrl"
                  path={'experience.' + i + '.url'}
                  value={e.url}
                  max={2048}
                  onChange={(v) => set({ ...e, url: v })}
                />
                <Move
                  index={i}
                  total={p.experience.length}
                  onMove={(to) => change('experience', reorder(p.experience, i, to))}
                  onRemove={() =>
                    change(
                      'experience',
                      p.experience.filter((x) => x.id !== e.id),
                    )
                  }
                />
              </fieldset>
            );
          })}
          <Button
            disabled={p.experience.length >= 20}
            onClick={() =>
              change('experience', [
                ...p.experience,
                {
                  id: id(),
                  role: empty(),
                  organization: empty(),
                  description: empty(),
                  start: '',
                  end: '',
                  current: true,
                  url: '',
                },
              ])
            }
          >
            <Label name="config.addExperience" />
          </Button>
        </Section>
        <Section name="config.contact">
          <TextField
            name="config.publicEmail"
            path="public_email"
            type="email"
            value={p.public_email}
            max={254}
            onChange={(v) => change('public_email', v)}
          />
          {(['location', 'copyright', 'footer'] as const).map((k) => (
            <LocalizedField
              key={k}
              name={'config.' + k}
              path={k}
              value={p[k]}
              max={k === 'location' ? 120 : k === 'copyright' ? 100 : 200}
              onChange={(v) => change(k, v)}
            />
          ))}
          <div data-config-path="about_key">
            <AboutSelect value={p.about_key} onChange={(v) => change('about_key', v)} />
          </div>
          <p className="vb-help">
            <Label name="config.aboutHelp" />
          </p>
        </Section>
        <Section name="config.sections">
          {(['show_now', 'show_writing', 'show_learning'] as const).map((k) => (
            <Check key={k} name={'config.' + k} value={p[k]} onChange={(v) => change(k, v)} />
          ))}
          {(['writing_limit', 'learning_limit'] as const).map((k) => (
            <label className="config-field" key={k}>
              <Label name={'config.' + k} />
              <input
                type="number"
                data-config-path={k}
                min={1}
                max={20}
                value={p[k]}
                onChange={(e) => change(k, Number(e.target.value))}
              />
            </label>
          ))}
        </Section>
      </div>
      <aside
        className="config-panel config-live-preview"
        aria-label={t('config.livePreview')}
        tabIndex={0}
      >
        <div className="config-preview-heading">
          <h2>{t('config.livePreview')}</h2>
        </div>
        <div className="config-preview-actions">
          {languages.map((l) => (
            <Button key={l} aria-pressed={previewLocale === l} onClick={() => setPreviewLocale(l)}>
              {t('language.' + l)}
            </Button>
          ))}
        </div>
        <ProfilePreview value={p} locale={previewLocale} monogram={data?.site.monogram} />
      </aside>
    </div>
  );
}
