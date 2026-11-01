import { useId, useRef, useState } from 'react';
import { Icon, type IconName } from './Icon';
import { useI18n } from '../i18n/I18nProvider';

export function FileDropInput({
  label,
  title = label,
  help,
  accept,
  disabled = false,
  choices,
  onFile,
}: {
  label: string;
  title?: string;
  help: string;
  accept: string;
  disabled?: boolean;
  choices?: { label: string; accept: string; icon?: IconName; quiet?: boolean }[];
  onFile: (file: File) => void;
}) {
  const { t } = useI18n();
  const id = useId();
  const picker = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  return (
    <div
      className={`vb-drop vb-file-drop${dragging ? ' dragging' : ''}`}
      role="group"
      aria-describedby={id}
      onDragOver={(e) => {
        e.preventDefault();
        if (!disabled && !e.currentTarget.closest('fieldset[disabled]')) {
          setDragging(true);
        }
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
          setDragging(false);
        }
      }}
      onDrop={(e) => {
        e.preventDefault();
        e.stopPropagation();
        setDragging(false);
        const file = e.dataTransfer.files[0];
        if (file && !disabled && !e.currentTarget.closest('fieldset[disabled]')) {
          onFile(file);
        }
      }}
    >
      <Icon name="upload" size={28} />
      <b>{title}</b>
      <span className="vb-help" id={id}>
        {help}
      </span>
      <div className="vb-file-choices">
        {(choices ?? [{ label: t('upload.chooseFile'), accept }]).map((choice) => (
          <button
            key={choice.accept}
            type="button"
            className={`vb-btn vb-btn-${'quiet' in choice && choice.quiet ? 'quiet' : 'secondary'} vb-btn-sm`}
            disabled={disabled}
            onClick={() => {
              if (picker.current) {
                picker.current.accept = choice.accept;
                picker.current.click();
              }
            }}
          >
            {'icon' in choice && choice.icon && <Icon name={choice.icon} />}
            {choice.label}
          </button>
        ))}
      </div>
      <input
        ref={picker}
        hidden
        type="file"
        aria-label={label}
        aria-describedby={id}
        accept={accept}
        disabled={disabled}
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (file) {
            onFile(file);
          }
          e.target.value = '';
        }}
      />
    </div>
  );
}
