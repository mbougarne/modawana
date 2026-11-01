import { useState } from 'react';
import { Button, Field } from '../../../../ui/src/components/primitives';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';
import { ReferencePicker } from './ReferencePicker';
import type { Locale, PlanMember } from '../../api/content';

export type SelectedMember = PlanMember & {
  expected_version?: number;
  title?: string;
  kind?: string;
};

export function CollectionContents({
  members,
  onChange,
  kind,
  locale,
  collectionKey,
  titles = {},
}: {
  members: SelectedMember[];
  onChange: (members: SelectedMember[]) => void;
  kind: 'learning' | 'docs';
  locale: Locale;
  collectionKey: string;
  titles?: Record<string, string>;
}) {
  const { t } = useI18n();
  const [selected, setSelected] = useState<{
    key: string;
    title: string;
    kind: string;
    version: number;
  }>();
  const [query, setQuery] = useState('');
  function move(index: number, delta: number) {
    const next = [...members];
    [next[index], next[index + delta]] = [next[index + delta], next[index]];
    onChange(next);
  }
  return (
    <section>
      <h2>{t('collection.contents')}</h2>
      <p className="vb-help">{t('collection.contentsHelp')}</p>
      <div className="owner-row">
        <ReferencePicker
          label={t('collection.addContent')}
          value={query}
          kind={kind === 'learning' ? 'learning_item' : 'docs_item'}
          locale={locale}
          collectionKey={collectionKey}
          excludeKeys={members.map((m) => m.key)}
          onChange={(key, choice) => {
            setQuery(key);
            setSelected(choice);
          }}
        />
        <Button
          disabled={
            !selected || members.some((m) => m.key === selected.key) || members.length >= 500
          }
          onClick={() => {
            if (!selected) {
              return;
            }
            onChange([
              ...members,
              {
                key: selected.key,
                title: selected.title,
                kind: selected.kind,
                expected_version: selected.version,
                announcements: {},
              },
            ]);
            setQuery('');
            setSelected(undefined);
          }}
        >
          {t('owner.add')}
        </Button>
      </div>
      {!members.length && <p className="vb-help">{t('collection.empty')}</p>}
      <ol>
        {members.map((member, index) => (
          <li key={member.key} className="owner-panel">
            <strong dir="auto">{member.title || titles[member.key] || member.key}</strong>
            <div className="owner-row">
              <Button disabled={index === 0} onClick={() => move(index, -1)}>
                {t('owner.up')}
              </Button>
              <Button disabled={index === members.length - 1} onClick={() => move(index, 1)}>
                {t('owner.down')}
              </Button>
              <Button onClick={() => onChange(members.filter((_, i) => i !== index))}>
                {t('owner.remove')}
              </Button>
            </div>
            <details>
              <summary>{t('collection.announcementOptions')}</summary>
              <p className="vb-help">{t('owner.announcementHelp')}</p>
              <bdi>{member.key}</bdi>
              {(['en', 'ar', 'fr'] as const).map((language) => (
                <Field
                  key={language}
                  label={t('owner.announcement') + ' · ' + t('language.' + language)}
                >
                  <input
                    lang={language}
                    dir="auto"
                    maxLength={200}
                    value={member.announcements[language] || ''}
                    onChange={(e) => {
                      const announcements = { ...member.announcements };
                      if (e.target.value) {
                        announcements[language] = e.target.value;
                      } else {
                        delete announcements[language];
                      }
                      onChange(members.map((m, i) => (i === index ? { ...m, announcements } : m)));
                    }}
                  />
                </Field>
              ))}
            </details>
          </li>
        ))}
      </ol>
    </section>
  );
}
