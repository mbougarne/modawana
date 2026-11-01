import { brandPresets } from '../tokens/tokens';
import { useI18n } from '../i18n/I18nProvider';

export function BrandColourField({
  role,
  value,
  onChange,
}: {
  role: 'primary' | 'secondary';
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useI18n();
  const presets = brandPresets[role];
  const valid = /^#[0-9a-f]{6}$/i.test(value);
  const name = t('config.' + role);
  return (
    <fieldset className="config-colour-field">
      <legend>{name}</legend>
      <div className="vb-colorrow">
        <input
          type="color"
          aria-label={name}
          value={valid ? value : presets[0].value}
          onChange={(e) => onChange(e.target.value.toUpperCase())}
        />
        <input
          className="vb-input"
          data-config-path={role}
          type="text"
          aria-label={t('colour.hex', { name })}
          aria-invalid={!valid}
          dir="ltr"
          spellCheck={false}
          autoComplete="off"
          maxLength={7}
          pattern="#[0-9a-fA-F]{6}"
          required
          value={value}
          onChange={(e) => onChange(e.target.value.toUpperCase())}
        />
      </div>
      <div className="vb-swatches" role="group" aria-label={t('colour.presets', { name })}>
        {presets.map((preset) => (
          <button
            key={preset.id}
            type="button"
            className={`vb-swatch brand-preset-${role}-${preset.id}`}
            aria-label={t('colour.preset', {
              name: t('colourName.' + preset.id),
              hex: preset.value,
            })}
            title={t('colourName.' + preset.id)}
            aria-pressed={value.toUpperCase() === preset.value}
            onClick={() => onChange(preset.value)}
          />
        ))}
      </div>
      {!valid && (
        <p role="alert" className="vb-help">
          {t('colour.invalid')}
        </p>
      )}
    </fieldset>
  );
}
