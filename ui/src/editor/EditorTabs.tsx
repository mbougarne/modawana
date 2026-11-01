import type { KeyboardEvent } from 'react';
import { useI18n } from '../i18n/I18nProvider';

export function EditorTabs({
  tabs,
  current,
  onChange,
}: {
  tabs: { key: string; label: string; count?: number }[];
  current: string;
  onChange: (key: string) => void;
}) {
  const { t, dir } = useI18n();
  function keydown(event: KeyboardEvent<HTMLDivElement>) {
    const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End'];
    if (!keys.includes(event.key)) {
      return;
    }
    const buttons = Array.from(
      event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
    );
    const index = buttons.indexOf(event.target as HTMLButtonElement);
    if (index < 0) {
      return;
    }
    event.preventDefault();
    const delta = (event.key === 'ArrowRight' ? 1 : -1) * (dir === 'rtl' ? -1 : 1);
    const next =
      event.key === 'Home'
        ? 0
        : event.key === 'End'
          ? buttons.length - 1
          : (index + delta + buttons.length) % buttons.length;
    buttons[next].focus();
    onChange(tabs[next].key);
  }
  return (
    <div
      className="vb-tabs editor-tabs"
      role="tablist"
      aria-label={t('owner.editor')}
      onKeyDown={keydown}
    >
      {tabs.map((tab) => (
        <button
          key={tab.key}
          type="button"
          role="tab"
          id={'tab-' + tab.key}
          aria-controls={'panel-' + tab.key}
          aria-label={tab.label}
          aria-selected={tab.key === current}
          tabIndex={tab.key === current ? 0 : -1}
          onClick={() => onChange(tab.key)}
        >
          {tab.label}
          {tab.count !== undefined && (
            <span className="editor-tab-count" aria-hidden="true">
              {tab.count}
            </span>
          )}
        </button>
      ))}
    </div>
  );
}
