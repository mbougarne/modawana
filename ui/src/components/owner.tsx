import { useRef, useState, type KeyboardEvent } from 'react';
import { useI18n } from '../i18n/I18nProvider';

/**
 * Six-box one-time code input (TOTP). Typing, paste and arrow keys work; onComplete fires on the sixth digit.
 * Put inside a <form>; the first box carries autocomplete="one-time-code".
 */
export function OtpInput({
  length = 6,
  onComplete,
  error,
}: {
  length?: number;
  onComplete: (code: string) => void;
  error?: boolean;
}) {
  const { t } = useI18n();
  const [digits, setDigits] = useState<string[]>(() => Array(length).fill(''));
  const refs = useRef<(HTMLInputElement | null)[]>([]);
  const set = (next: string[]) => {
    setDigits(next);
    if (next.every(Boolean)) {
      onComplete(next.join(''));
    }
  };
  const onInput = (i: number, v: string) => {
    const d = v.replace(/\D/g, '');
    if (d.length > 1) {
      const next = [...digits];
      d.slice(0, length - i)
        .split('')
        .forEach((c, j) => {
          next[i + j] = c;
        });
      set(next);
      refs.current[Math.min(i + d.length, length - 1)]?.focus();
      return;
    }
    const next = [...digits];
    next[i] = d;
    set(next);
    if (d && i < length - 1) {
      refs.current[i + 1]?.focus();
    }
  };
  const onKey = (i: number, e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Backspace' && !digits[i] && i > 0) {
      refs.current[i - 1]?.focus();
    }
    if (e.key === 'ArrowLeft' && i > 0) {
      refs.current[i - 1]?.focus();
    }
    if (e.key === 'ArrowRight' && i < length - 1) {
      refs.current[i + 1]?.focus();
    }
  };
  return (
    <div
      className="vb-otp"
      dir="ltr"
      role="group"
      aria-label={t('login.codeTitle')}
      data-error={error ? 'true' : undefined}
    >
      {digits.map((d, i) => (
        <input
          key={i}
          ref={(el) => {
            refs.current[i] = el;
          }}
          value={d}
          inputMode="numeric"
          autoComplete={i === 0 ? 'one-time-code' : 'off'}
          maxLength={i === 0 ? length : 1}
          aria-label={t('login.digit', { n: i + 1 })}
          onChange={(e) => onInput(i, e.target.value)}
          onKeyDown={(e) => onKey(i, e)}
        />
      ))}
    </div>
  );
}

const normalizeTag = (raw: string) =>
  raw
    .toLowerCase()
    .replace(/^#/, '')
    .trim()
    .replace(/\s+/g, '-')
    .replace(/[^\p{L}\p{N}-]/gu, '');

/** Tag field for the editor: tokens, Enter/comma to add, Backspace to remove, suggestions, max count. */
export function TagInput({
  value,
  onChange,
  suggestions = [],
  max = 8,
}: {
  value: string[];
  onChange: (tags: string[]) => void;
  suggestions?: string[];
  max?: number;
}) {
  const { t } = useI18n();
  const [text, setText] = useState('');
  const add = (raw: string) => {
    const x = normalizeTag(raw);
    if (x && !value.includes(x) && value.length < max) {
      onChange([...value, x]);
    }
    setText('');
  };
  return (
    <div className="vb-taginput-field">
      <div className="vb-taginput">
        {value.map((x) => (
          <span className="vb-tok" key={x} dir="ltr">
            #{x}
            <button
              type="button"
              aria-label={t('tag.remove', { tag: x })}
              onClick={() => onChange(value.filter((y) => y !== x))}
            >
              ×
            </button>
          </span>
        ))}
        <input
          value={text}
          dir="ltr"
          placeholder={t('tag.add')}
          aria-label={t('tag.add')}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ',') {
              e.preventDefault();
              add(text);
            } else if (e.key === 'Backspace' && !text && value.length) {
              onChange(value.slice(0, -1));
            }
          }}
        />
      </div>
      <div className="vb-suggest">
        {suggestions
          .filter((s) => !value.includes(s))
          .map((s) => (
            <button type="button" key={s} dir="ltr" onClick={() => add(s)}>
              + #{s}
            </button>
          ))}
      </div>
      <span className="vb-help">
        {t('tag.max', { n: max })} · {value.length}/{max}
      </span>
    </div>
  );
}
