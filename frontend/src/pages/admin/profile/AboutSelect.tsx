import { useEffect, useState } from 'react';
import { Button, Label, useI18n } from '@ui';
import { call } from '../../../features/auth/api';
import type { DiscoveryEntry, DiscoveryPage } from '../../../api/content';

export function AboutSelect({
  value,
  onChange,
}: {
  value: string;
  onChange: (key: string) => void;
}) {
  const { t, locale } = useI18n();
  const [items, setItems] = useState<DiscoveryEntry[]>([]);
  const [cursor, setCursor] = useState('');
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let active = true;
    setBusy(true);
    setFailed(false);
    setItems([]);
    setCursor('');
    call<DiscoveryPage>('/public/content?kind=doc&locale=' + locale + '&limit=50')
      .then(
        (page) => {
          if (active) {
            setItems(page.items);
            setCursor(page.next_cursor);
          }
        },
        () => {
          if (active) {
            setFailed(true);
          }
        },
      )
      .finally(() => {
        if (active) {
          setBusy(false);
        }
      });
    return () => {
      active = false;
    };
  }, [locale]);
  async function more() {
    setBusy(true);
    setFailed(false);
    try {
      const page = await call<DiscoveryPage>(
        '/public/content?kind=doc&locale=' +
          locale +
          '&limit=50&cursor=' +
          encodeURIComponent(cursor),
      );
      setItems((old) => [...old, ...page.items]);
      setCursor(page.next_cursor);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <label className="config-field">
        <Label name="config.aboutKey" />
        <select value={value} disabled={busy} onChange={(e) => onChange(e.target.value)}>
          <option value="">{t('config.none')}</option>
          {value && !items.some((x) => x.key === value) && <option value={value}>{value}</option>}
          {items.map((x) => (
            <option key={x.key} value={x.key}>
              {x.title}
            </option>
          ))}
        </select>
      </label>
      {cursor && (
        <Button disabled={busy} onClick={() => void more()}>
          <Label name="config.more" />
        </Button>
      )}
      {failed && (
        <p role="alert">
          <Label name="config.invalid" />
        </p>
      )}
    </>
  );
}
