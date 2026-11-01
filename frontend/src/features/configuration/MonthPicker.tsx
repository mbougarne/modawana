import { useId, useRef, useState } from 'react';
import { Button, useI18n } from '@ui';
import { useFieldIssue } from './ValidationIssues';

export function MonthPicker({
  name,
  path,
  value,
  onChange,
}: {
  name: string;
  path?: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { t, locale } = useI18n();
  const issue = useFieldIssue(path);
  const id = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [year, setYear] = useState(Number(value.slice(0, 4)) || new Date().getFullYear());
  const monthName = (month: number) =>
    new Intl.DateTimeFormat(locale, { month: 'long', timeZone: 'UTC' }).format(
      new Date(Date.UTC(2000, month, 1)),
    );
  function choose(next: string) {
    onChange(next);
    setOpen(false);
    trigger.current?.focus();
  }
  return (
    <div
      className="config-month-picker"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          setOpen(false);
        }
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.preventDefault();
          setOpen(false);
          trigger.current?.focus();
        }
      }}
    >
      <span id={id + '-label'} className="vb-label">
        {t(name)}
      </span>
      <button
        ref={trigger}
        type="button"
        className="vb-btn vb-btn-secondary config-month-trigger"
        {...issue}
        aria-labelledby={id + '-label ' + id + '-value'}
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => {
          if (!open) {
            setYear(Number(value.slice(0, 4)) || new Date().getFullYear());
          }
          setOpen(!open);
        }}
      >
        <span id={id + '-value'}>
          {value
            ? monthName(Number(value.slice(5)) - 1) + ' ' + value.slice(0, 4)
            : t('date.chooseMonth')}
        </span>
      </button>
      {open && (
        <section id={id} className="config-month-panel" aria-labelledby={id + '-label'}>
          <div className="config-month-year">
            <Button
              aria-label={t('date.previousYear')}
              disabled={year <= 1900}
              onClick={() => setYear(year - 1)}
            >
              −
            </Button>
            <label>
              {t('date.year')}
              <input
                type="number"
                min={1900}
                max={2099}
                value={year}
                onChange={(event) => {
                  const next = Number(event.target.value);
                  if (Number.isInteger(next) && next >= 1900 && next <= 2099) {
                    setYear(next);
                  }
                }}
              />
            </label>
            <Button
              aria-label={t('date.nextYear')}
              disabled={year >= 2099}
              onClick={() => setYear(year + 1)}
            >
              +
            </Button>
          </div>
          <div className="config-month-grid">
            {Array.from({ length: 12 }, (_, month) => {
              const next = year + '-' + String(month + 1).padStart(2, '0');
              return (
                <Button key={month} aria-pressed={value === next} onClick={() => choose(next)}>
                  {monthName(month)}
                </Button>
              );
            })}
          </div>
          <Button onClick={() => choose('')}>{t('date.clear')}</Button>
        </section>
      )}
    </div>
  );
}
