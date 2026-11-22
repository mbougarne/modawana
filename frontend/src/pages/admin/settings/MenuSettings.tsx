import { Icon, useI18n, type IconName } from '@ui';
import type { SiteSettings } from '../../../api/configuration';
import { Section } from '../../../features/configuration/controls';
import { iconNames } from './siteBrand';

export function MenuSettings({
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
