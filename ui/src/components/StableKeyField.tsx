import { useRef } from 'react';
import { Field, IconButton } from './primitives';
import { Icon } from './Icon';
import { useI18n } from '../i18n/I18nProvider';

export function generateStableKey(): string {
  return 'key-' + crypto.randomUUID();
}

export function StableKeyField({
  value,
  onChange,
  disabled = false,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const { t } = useI18n();
  const input = useRef<HTMLInputElement>(null);
  return (
    <div className="stable-key-field">
      <Field label={t('owner.key')}>
        <input
          ref={input}
          value={value}
          dir="ltr"
          maxLength={80}
          pattern="[a-z0-9][a-z0-9_-]*"
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
        />
      </Field>
      {!disabled && (
        <IconButton
          label={t('owner.generateKey')}
          disabled={disabled}
          onClick={() => {
            onChange(generateStableKey());
            input.current?.focus();
          }}
        >
          <Icon name="generate" />
        </IconButton>
      )}
    </div>
  );
}
