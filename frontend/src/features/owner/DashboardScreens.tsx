import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, useI18n } from '@ui';
import { call } from '../auth/api';
import { localizedContribution, contributionKey, useExtensionCatalog } from './ExtensionCatalog';
import { OwnerPageHeading } from './OwnerWorkspace';
import { useOwnerAppearanceContext } from './OwnerAppearance';
import {
  appliedLayout,
  dashboardGroups,
  orderedItems,
  useDashboardLayout,
  type DashboardAppearance,
  type DashboardChoice,
  type DashboardItem,
  type DashboardLayout,
} from './DashboardLayout';

const builtinWidgets = ['content', 'site'];

export function OwnerDashboard() {
  const { t, locale } = useI18n();
  const { state } = useDashboardLayout();
  const { items } = useExtensionCatalog();
  const widgets = items.filter((item) => item.kind === 'widget' && item.text);
  const registered = [
    ...builtinWidgets.map((id) => ({ id, label: t('ownerLayout.widget.' + id), text: '' })),
    ...widgets.map((item) => ({
      id: 'extension:' + contributionKey(item),
      label: localizedContribution(item.label, locale),
      text: item.text ? localizedContribution(item.text, locale) : '',
    })),
  ];
  const display = orderedItems(registered, appliedLayout(state)?.widgets).filter(
    ({ item }) => !item.hidden,
  );
  return (
    <>
      <OwnerPageHeading title={t('owner.dashboard')}>
        <Link to={`/${locale}/admin/layout`}>{t('owner.layout')}</Link>
      </OwnerPageHeading>
      {display.map(({ target, item }) => (
        <section className="owner-panel" key={target.id}>
          <h2 dir="auto">{item.rename?.[locale] || item.rename?.en || target.label}</h2>
          {target.id === 'content' && (
            <div className="owner-fields">
              <Link to={`/${locale}/admin/new?kind=page`}>{t('owner.newPage')}</Link>
              <Link to={`/${locale}/admin/posts`}>{t('owner.posts')}</Link>
            </div>
          )}
          {target.id === 'site' && (
            <div className="owner-fields">
              <a href={`/${locale}`}>{t('auth.viewSite')}</a>
              <Link to={`/${locale}/admin/settings`}>{t('owner.settings')}</Link>
            </div>
          )}
          {target.text && <p dir="auto">{target.text}</p>}
        </section>
      ))}
    </>
  );
}

export function DashboardLayoutSettings({ csrf }: { csrf: string }) {
  const { state, error, reload } = useDashboardLayout();
  const { clearTheme } = useOwnerAppearanceContext();
  const { t, locale } = useI18n();
  const [scope, setScope] = useState<'site' | 'personal'>('personal');
  const [draft, setDraft] = useState<DashboardLayout>();
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState('');
  const [presetName, setPresetName] = useState('');
  if (!state) {
    return (
      <>
        <OwnerPageHeading title={t('owner.layout')} />
        {error && <p role="alert">{t('ownerLayout.failed')}</p>}
      </>
    );
  }
  const record = state[scope];
  const selected = draft ?? record.layout ?? (scope === 'personal' ? state.site.layout : null);
  const layout: DashboardLayout = selected ?? { appearance: {} };
  const navigation = orderedItems(state.registry.navigation, layout.navigation);
  const widgets = orderedItems(state.registry.widgets, layout.widgets);
  const choiceLabel = (choice: DashboardChoice, kind: 'navigation' | 'widgets') =>
    choice.label?.[locale] ||
    choice.label?.en ||
    t((kind === 'navigation' ? 'owner.' : 'ownerLayout.widget.') + choice.id);

  async function save(reset = false) {
    setSaving(true);
    setNotice('');
    try {
      await call(
        '/dashboard-layout',
        'PUT',
        {
          scope,
          expected_version: record.version,
          reset,
          layout: reset ? null : layout,
        },
        csrf,
      );
      setDraft(undefined);
      if (scope === 'personal') {
        clearTheme();
      }
      setNotice('ownerLayout.saved');
    } catch {
      setNotice('ownerLayout.failed');
    } finally {
      setSaving(false);
      reload();
    }
  }
  function changeAppearance(appearance: DashboardAppearance) {
    setDraft({ ...layout, appearance });
  }
  return (
    <>
      <OwnerPageHeading title={t('owner.layout')} description={t('ownerLayout.help')} />
      <div className="owner-panel owner-form">
        <fieldset disabled={saving} className="owner-form">
          <label>
            {t('ownerLayout.scope')}
            <select
              value={scope}
              onChange={(event) => {
                setScope(event.target.value === 'site' ? 'site' : 'personal');
                setDraft(undefined);
                setNotice('');
              }}
            >
              <option value="personal">{t('ownerLayout.personal')}</option>
              <option value="site">{t('ownerLayout.site')}</option>
            </select>
          </label>
          <label>
            {t('ownerLayout.home')}
            <select
              value={layout.home ?? ''}
              onChange={(e) => setDraft({ ...layout, home: e.target.value })}
            >
              <option value="">{t('owner.security')}</option>
              <option value="dashboard">{t('owner.dashboard')}</option>
              {navigation
                .filter(({ target, item }) => !item.hidden && !target.id.startsWith('extension:'))
                .map(({ target }) => (
                  <option key={target.id} value={target.id}>
                    {choiceLabel(target, 'navigation')}
                  </option>
                ))}
            </select>
          </label>
          <div className="owner-fields">
            <label>
              {t('ownerAppearance.theme')}
              <select
                value={layout.appearance.theme || ''}
                onChange={(e) => {
                  const theme = e.target.value;
                  changeAppearance({
                    ...layout.appearance,
                    theme: theme === 'slate' || theme === 'classic' ? theme : undefined,
                  });
                }}
              >
                <option value="">{t('ownerLayout.default')}</option>
                <option value="classic">{t('ownerAppearance.classic')}</option>
                <option value="slate">{t('ownerAppearance.slate')}</option>
              </select>
            </label>
            <label>
              {t('ownerLayout.density')}
              <select
                value={layout.appearance.density || 'comfortable'}
                onChange={(e) =>
                  changeAppearance({
                    ...layout.appearance,
                    density: e.target.value === 'compact' ? 'compact' : 'comfortable',
                  })
                }
              >
                <option value="comfortable">{t('ownerLayout.comfortable')}</option>
                <option value="compact">{t('ownerLayout.compact')}</option>
              </select>
            </label>
          </div>
          <section>
            <h2>{t('ownerLayout.presets')}</h2>
            {layout.presets?.map((preset) => (
              <div className="owner-fields" key={preset.name}>
                <Button variant="quiet" onClick={() => changeAppearance(preset.appearance)}>
                  {preset.name}
                </Button>
                <Button
                  variant="quiet"
                  aria-label={t('ownerLayout.removePreset', { name: preset.name })}
                  onClick={() =>
                    setDraft({
                      ...layout,
                      presets: layout.presets?.filter((row) => row.name !== preset.name),
                    })
                  }
                >
                  {t('config.remove')}
                </Button>
              </div>
            ))}
            <div className="owner-fields">
              <label>
                {t('ownerLayout.presetName')}{' '}
                <input
                  maxLength={60}
                  value={presetName}
                  onChange={(e) => setPresetName(e.target.value)}
                />
              </label>
              <Button
                disabled={
                  !presetName.trim() ||
                  (layout.presets?.length ?? 0) >= 8 ||
                  layout.presets?.some(
                    (row) => row.name.toLowerCase() === presetName.trim().toLowerCase(),
                  )
                }
                onClick={() => {
                  setDraft({
                    ...layout,
                    presets: [
                      ...(layout.presets || []),
                      { name: presetName.trim(), appearance: layout.appearance },
                    ],
                  });
                  setPresetName('');
                }}
              >
                {t('ownerLayout.savePreset')}
              </Button>
            </div>
          </section>
          {(['navigation', 'widgets'] as const).map((kind) => (
            <section key={kind}>
              <h2>{t('ownerLayout.' + kind)}</h2>
              <LayoutItems
                entries={kind === 'navigation' ? navigation : widgets}
                label={(choice) => choiceLabel(choice, kind)}
                group={kind === 'navigation'}
                onChange={(items) => setDraft({ ...layout, [kind]: items })}
                home={kind === 'navigation' ? layout.home : undefined}
              />
            </section>
          ))}
          <div className="owner-fields">
            <Button onClick={() => void save()} disabled={!draft}>
              {t('ownerLayout.save')}
            </Button>
            <Button
              variant="quiet"
              onClick={() => void save(true)}
              disabled={scope === 'site' && !record.layout}
            >
              {t('ownerLayout.reset')}
            </Button>
          </div>
        </fieldset>
        {notice && <p role="status">{t(notice)}</p>}
      </div>
    </>
  );
}

function LayoutItems({
  entries,
  label,
  group,
  home,
  onChange,
}: {
  entries: { target: DashboardChoice; item: DashboardItem }[];
  label: (choice: DashboardChoice) => string;
  group: boolean;
  home?: string;
  onChange: (items: DashboardItem[]) => void;
}) {
  const { t, locale } = useI18n();
  return (
    <ol>
      {entries.map(({ target, item }, index) => {
        const name = label(target);
        const update = (next: DashboardItem) =>
          onChange(entries.map((entry, i) => (i === index ? next : entry.item)));
        const move = (offset: number) => {
          const items = entries.map((entry) => entry.item);
          [items[index], items[index + offset]] = [items[index + offset], items[index]];
          onChange(items);
        };
        return (
          <li key={item.id}>
            <fieldset className="owner-fields">
              <legend dir="auto">{name}</legend>
              <label>
                {t('ownerLayout.rename')}
                <input
                  dir="auto"
                  maxLength={60}
                  value={item.rename?.[locale] ?? ''}
                  onChange={(e) => {
                    const rename = { ...item.rename };
                    if (e.target.value.trim()) {
                      rename[locale] = e.target.value;
                    } else {
                      delete rename[locale];
                    }
                    update({ ...item, rename });
                  }}
                />
              </label>
              {group && (
                <label>
                  {t('ownerLayout.group')}
                  <select
                    value={item.group ?? ''}
                    onChange={(e) => update({ ...item, group: e.target.value })}
                  >
                    <option value="">{t('ownerLayout.default')}</option>
                    {dashboardGroups.map((group) => (
                      <option key={group} value={group}>
                        {t('ownerLayout.group.' + group)}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              <label>
                <input
                  type="checkbox"
                  checked={!!item.hidden}
                  disabled={item.id === home}
                  onChange={(e) => update({ ...item, hidden: e.target.checked })}
                />
                {t('ownerLayout.hide')}
              </label>
              <Button
                variant="quiet"
                disabled={index === 0}
                onClick={() => move(-1)}
                aria-label={t('ownerLayout.up', { name })}
              >
                {t('ownerLayout.moveUp')}
              </Button>
              <Button
                variant="quiet"
                disabled={index === entries.length - 1}
                onClick={() => move(1)}
                aria-label={t('ownerLayout.down', { name })}
              >
                {t('ownerLayout.moveDown')}
              </Button>
            </fieldset>
          </li>
        );
      })}
    </ol>
  );
}
