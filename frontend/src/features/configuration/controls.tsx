import { MonthPicker } from './MonthPicker';
import { preparePortrait } from './portrait';
import { useFieldIssue } from './ValidationIssues';
import {
  cloneElement,
  type ReactElement,
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { Button, Field, TextInput, useI18n, Label } from '@ui';
import type { Localized } from '../../api/configuration';
import type { Locale } from '../../api/content';
import type { MediaRecord, MediaPage } from '../../api/contracts';
import { FileDropInput } from '../../../../ui/src/components/FileDropInput';
import { call } from '../auth/api';

export const UploadActivity = createContext<(delta: number) => void>(() => {});

export const languages: Locale[] = ['en', 'fr', 'ar'];

export function TextField({
  name,
  path,
  value,
  onChange,
  max = 200,
  type = 'text',
}: {
  name: string;
  path?: string;
  value: string;
  onChange: (v: string) => void;
  max?: number;
  type?: string;
}) {
  const { t } = useI18n();
  const issue = useFieldIssue(path);
  if (type === 'month') {
    return <MonthPicker name={name} path={path} value={value} onChange={onChange} />;
  }
  return (
    <Field label={t(name)}>
      <TextInput
        {...issue}
        type={type}
        value={value}
        maxLength={max}
        onChange={(e) => onChange(e.target.value)}
      />
    </Field>
  );
}

export function Check({
  name,
  value,
  onChange,
}: {
  name: string;
  value: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="config-check">
      <input type="checkbox" checked={value} onChange={(e) => onChange(e.target.checked)} />
      <Label name={name} />
    </label>
  );
}

function LocalizedControlIssue({
  path,
  children,
  ...binding
}: {
  path?: string;
  children: ReactElement<Record<string, unknown>>;
  id?: string;
  'aria-describedby'?: string;
}) {
  const issue = useFieldIssue(path);
  return cloneElement(children, {
    ...binding,
    ...issue,
    'aria-describedby':
      [binding['aria-describedby'], issue['aria-describedby']].filter(Boolean).join(' ') ||
      undefined,
  });
}

export function LocalizedField({
  name,
  path,
  value,
  onChange,
  max = 200,
  multiline = false,
}: {
  name: string;
  path?: string;
  value: Localized;
  onChange: (v: Localized) => void;
  max?: number;
  multiline?: boolean;
}) {
  const { t } = useI18n();
  return (
    <fieldset className="config-localized">
      <legend>
        <Label name={name} />
      </legend>
      {languages.map((l) => (
        <Field key={l} label={t('language.' + l)}>
          {multiline ? (
            <LocalizedControlIssue path={path ? path + '.' + l : undefined}>
              <textarea
                lang={l}
                dir={l === 'ar' ? 'rtl' : 'ltr'}
                value={value?.[l] ?? ''}
                maxLength={max}
                onChange={(e) => onChange({ ...value, [l]: e.target.value })}
              />
            </LocalizedControlIssue>
          ) : (
            <LocalizedControlIssue path={path ? path + '.' + l : undefined}>
              <TextInput
                lang={l}
                dir={l === 'ar' ? 'rtl' : 'ltr'}
                value={value?.[l] ?? ''}
                maxLength={max}
                onChange={(e) => onChange({ ...value, [l]: e.target.value })}
              />
            </LocalizedControlIssue>
          )}
        </Field>
      ))}
    </fieldset>
  );
}

export function Section({ name, children }: { name: string; children: ReactNode }) {
  return (
    <section className="config-panel">
      <h2>
        <Label name={name} />
      </h2>
      {children}
    </section>
  );
}

export function MediaInput({
  name,
  purpose,
  value,
  onChange,
  csrf,
}: {
  name: string;
  purpose: 'logo' | 'favicon' | 'portrait' | 'banner';
  value: string;
  onChange: (v: string) => void;
  csrf: string;
}) {
  const issue = useFieldIssue(purpose + '_media_id');
  const activity = useContext(UploadActivity);
  const changed = useRef(onChange);
  changed.current = onChange;
  const request = useRef<XMLHttpRequest | null>(null);
  useEffect(
    () => () => {
      request.current?.abort();
    },
    [],
  );
  const { t } = useI18n();
  const [items, setItems] = useState<MediaRecord[]>([]);
  const [next, setNext] = useState('');
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState('');
  async function load(cursor = '') {
    try {
      const page = await call<MediaPage>('/media?kind=image&cursor=' + cursor);
      setItems((old) => (cursor ? [...old, ...page.items] : page.items));
      setNext(page.next_cursor);
    } catch {
      setError('config.invalidUpload');
    }
  }
  async function upload(file: File) {
    if (busy) {
      return;
    }
    setBusy(true);
    activity(1);
    setError('');
    if (purpose === 'portrait') {
      try {
        file = await preparePortrait(file);
      } catch (error) {
        setError(
          error instanceof Error && error.message === 'validation.portrait_media'
            ? error.message
            : 'config.invalidUpload',
        );
        setBusy(false);
        activity(-1);
        return;
      }
    }
    const max = (purpose === 'portrait' ? 2 : purpose === 'banner' ? 10 : 1) * 1048576;
    if (file.size > max) {
      setError('config.invalidUpload');
      setBusy(false);
      activity(-1);
      return;
    }
    setProgress(0);
    const form = new FormData();
    form.append('file', file);
    await new Promise<void>((resolve) => {
      const x = new XMLHttpRequest();
      request.current = x;
      x.timeout = 60000;
      x.open('POST', '/api/v1/media?purpose=' + purpose);
      x.setRequestHeader('X-CSRF-Token', csrf);
      x.upload.onprogress = (e) => {
        if (e.lengthComputable) {
          setProgress(Math.round((e.loaded / e.total) * 100));
        }
      };
      x.onload = () => {
        try {
          if (x.status !== 201) {
            throw Error();
          }
          const m = JSON.parse(x.responseText) as MediaRecord;
          setItems((old) => [m, ...old]);
          changed.current(m.id);
        } catch {
          setError('config.invalidUpload');
        }
        resolve();
      };
      x.onerror = x.ontimeout = () => {
        setError('config.invalidUpload');
        resolve();
      };
      x.onabort = () => resolve();
      x.send(form);
    });
    setBusy(false);
    activity(-1);
  }
  return (
    <fieldset className="config-media" {...issue}>
      <legend>
        <Label name={name} />
      </legend>
      {value && <img src={'/media/' + value} alt={t(name)} />}
      <FileDropInput
        label={t('config.upload')}
        choices={[
          {
            label: t('upload.chooseImage'),
            accept: 'image/png,image/jpeg,image/webp',
            icon: 'image',
          },
        ]}
        help={t('config.uploadLimit', {
          n: purpose === 'portrait' || purpose === 'banner' ? 10 : 1,
        })}
        accept="image/png,image/jpeg,image/webp"
        disabled={busy}
        onFile={(file) => void upload(file)}
      />
      {busy && <progress aria-label={t('config.upload')} max={100} value={progress} />}
      <Button onClick={() => load()}>
        <Label name="config.chooseExisting" />
      </Button>
      {items.length > 0 && (
        <Field label={t('config.chooseExisting')}>
          <select value={value} onChange={(e) => onChange(e.target.value)}>
            <option value="">{t('config.none')}</option>
            {value && !items.some((x) => x.id === value) && (
              <option value={value}>{t('config.currentImage')}</option>
            )}
            {items.map((m) => (
              <option key={m.id} value={m.id}>
                {m.filename}
              </option>
            ))}
          </select>
        </Field>
      )}
      {next && (
        <Button onClick={() => load(next)}>
          <Label name="config.more" />
        </Button>
      )}
      {value && (
        <Button onClick={() => onChange('')}>
          <Label name="config.remove" />
        </Button>
      )}
      {error && <p role="alert">{t(error)}</p>}
    </fieldset>
  );
}
