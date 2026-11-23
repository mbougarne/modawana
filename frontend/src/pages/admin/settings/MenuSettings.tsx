import { localeCodes } from '../../../../../ui/src/i18n/locales';
import { Button, Icon, Label, useI18n, type IconName } from '@ui';
import type { MenuItem, SiteSettings } from '../../../api/configuration';
import { LocalizedField, Section, TextField } from '../../../features/configuration/controls';
import { useFieldIssue } from '../../../features/configuration/ValidationIssues';
import { ReferencePicker } from '../../../features/owner/ReferencePicker';
import { iconNames } from './siteBrand';

const destinations = ['home', 'writing', 'learning', 'docs', 'about', 'page', 'custom'] as const;
const defaultFooter = ['writing', 'learning', 'docs'] as const;
const defaultHeader = [...defaultFooter, 'about'] as const;

export function MenuSettings({
  value,
  onChange,
}: {
  value: SiteSettings;
  onChange: (settings: SiteSettings) => void;
}) {
  const { t } = useI18n();
  return (
    <>
      <Section name="config.frontPages">
        <p>{t('config.frontPagesHelp')}</p>
        {localeCodes.map((locale) => (
          <ReferencePicker
            key={locale}
            label={t('language.' + locale)}
            value={value.front_pages?.[locale] ?? ''}
            kind="page"
            locale={locale}
            path={'front_pages.' + locale}
            onChange={(key) => {
              const front_pages = { ...value.front_pages };
              if (key) {
                front_pages[locale] = key;
              } else {
                delete front_pages[locale];
              }
              onChange({ ...value, front_pages });
            }}
          />
        ))}
      </Section>
      <NavigationMenu
        path="header_menu"
        name="config.headerMenu"
        value={value.header_menu}
        onChange={(header_menu) => onChange({ ...value, header_menu })}
      />
      <NavigationMenu
        path="footer_menu"
        name="config.footerMenu"
        value={value.footer_menu}
        onChange={(footer_menu) => onChange({ ...value, footer_menu })}
      />
      <MenuIcons
        value={value.menu_icons}
        onChange={(menu_icons) => onChange({ ...value, menu_icons })}
      />
    </>
  );
}

function NavigationMenu({
  path,
  name,
  value,
  onChange,
}: {
  path: 'header_menu' | 'footer_menu';
  name: string;
  value: MenuItem[] | undefined;
  onChange: (items: MenuItem[] | undefined) => void;
}) {
  const { t } = useI18n();
  const issue = useFieldIssue(path);
  const defaults = path === 'header_menu' ? defaultHeader : defaultFooter;
  const items = value ?? defaults.map((target) => ({ id: target, target }));
  const capacity =
    12 - items.reduce((total, item: MenuItem) => total + 1 + (item.children?.length ?? 0), 0);

  return (
    <Section name={name}>
      <div className="config-menu-editor" {...issue}>
        <p>{t(path === 'header_menu' ? 'config.headerMenuHelp' : 'config.footerMenuHelp')}</p>
        {items.length === 0 && <p>{t('config.menuEmpty')}</p>}
        <MenuItems items={items} onChange={onChange} path={path} capacity={capacity} />
        <div className="config-actions">
          <Button
            type="button"
            disabled={capacity <= 0}
            onClick={() => onChange([...items, { id: crypto.randomUUID(), target: 'custom' }])}
          >
            <Label name="config.addLink" />
          </Button>
          <Button
            type="button"
            variant="quiet"
            disabled={!value}
            onClick={() => onChange(undefined)}
          >
            <Label name="config.useDefaultMenu" />
          </Button>
        </div>
      </div>
    </Section>
  );
}

function MenuItems({
  items,
  path,
  onChange,
  capacity,
  nested = false,
}: {
  items: MenuItem[];
  path: string;
  onChange: (items: MenuItem[]) => void;
  capacity: number;
  nested?: boolean;
}) {
  const { t } = useI18n();
  if (items.length === 0) {
    return null;
  }
  return (
    <ol className="config-menu-items">
      {items.map((item, index) => {
        const update = (next: MenuItem) =>
          onChange(items.map((row, i) => (i === index ? next : row)));
        const children = item.children || [];
        return (
          <li key={item.id}>
            <MenuRow
              item={item}
              path={`${path}.${index}`}
              items={items}
              index={index}
              onChange={update}
              onMove={(to) => {
                const next = [...items];
                [next[to], next[index]] = [next[index], next[to]];
                onChange(next);
              }}
              onRemove={() => onChange(items.filter((_, i) => i !== index))}
            />
            {!nested && (
              <div className="config-menu-children">
                <MenuItems
                  items={children}
                  path={`${path}.${index}.children`}
                  onChange={(children) => update({ ...item, children })}
                  capacity={capacity}
                  nested
                />
                <Button
                  type="button"
                  variant="quiet"
                  disabled={capacity <= 0}
                  onClick={() =>
                    update({
                      ...item,
                      children: [...children, { id: crypto.randomUUID(), target: 'custom' }],
                    })
                  }
                >
                  {t('config.addSubmenuLink')}
                </Button>
              </div>
            )}
          </li>
        );
      })}
    </ol>
  );
}

function MenuRow({
  item,
  path,
  items,
  index,
  onChange,
  onMove,
  onRemove,
}: {
  item: MenuItem;
  path: string;
  items: MenuItem[];
  index: number;
  onChange: (item: MenuItem) => void;
  onMove: (index: number) => void;
  onRemove: () => void;
}) {
  const { t, locale } = useI18n();
  const issue = useFieldIssue(path + '.target');
  return (
    <fieldset className="config-menu-item">
      <legend>{t('config.row', { n: index + 1 })}</legend>
      <label className="config-field">
        <Label name="config.menuTarget" />
        <select
          {...issue}
          value={item.target}
          onChange={(event) =>
            onChange({
              id: item.id,
              children: item.children,
              target: event.target.value as MenuItem['target'],
            })
          }
        >
          {destinations.map((target) => (
            <option
              key={target}
              value={target}
              disabled={
                target !== 'custom' &&
                target !== 'page' &&
                target !== item.target &&
                items.some((row) => row.target === target)
              }
            >
              {t(
                target === 'page'
                  ? 'owner.page'
                  : target === 'custom'
                    ? 'config.customMenuLink'
                    : 'slot.' + target,
              )}
            </option>
          ))}
        </select>
      </label>
      {item.target === 'page' && (
        <ReferencePicker
          label={t('config.menuPage')}
          help={t('config.menuPageHelp')}
          value={item.page_key ?? ''}
          kind="page"
          locale={locale}
          path={path + '.page_key'}
          onChange={(page_key) => onChange({ ...item, page_key })}
        />
      )}
      {item.target === 'custom' && (
        <>
          <TextField
            name="config.url"
            path={path + '.url'}
            value={item.url ?? ''}
            max={2048}
            onChange={(url) => onChange({ ...item, url })}
          />
          <LocalizedField
            name="config.linkLabel"
            path={path + '.label'}
            value={item.label ?? {}}
            max={80}
            onChange={(label) => onChange({ ...item, label })}
          />
        </>
      )}
      <div className="config-actions">
        <Button type="button" disabled={index === 0} onClick={() => onMove(index - 1)}>
          <Label name="config.up" />
        </Button>
        <Button
          type="button"
          disabled={index === items.length - 1}
          onClick={() => onMove(index + 1)}
        >
          <Label name="config.down" />
        </Button>
        <Button type="button" variant="quiet" onClick={onRemove}>
          <Label name="config.remove" />
        </Button>
      </div>
    </fieldset>
  );
}

function MenuIcons({
  value,
  onChange,
}: {
  value: SiteSettings['menu_icons'];
  onChange: (icons: SiteSettings['menu_icons']) => void;
}) {
  const { t } = useI18n();
  return (
    <Section name="config.menuIcons">
      {Object.entries(value).map(([slot, icon]) => (
        <label className="config-field" key={slot}>
          <span>
            {t('slot.' + slot)} <Icon name={icon as IconName} />
          </span>
          <select
            data-config-path={'menu_icons.' + slot}
            value={icon}
            onChange={(event) => onChange({ ...value, [slot]: event.target.value })}
          >
            {iconNames.map((name) => (
              <option key={name} value={name}>
                {t('icon.' + name)}
              </option>
            ))}
          </select>
        </label>
      ))}
    </Section>
  );
}
