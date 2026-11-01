import { createContext, useContext, useEffect, useRef, type RefObject } from 'react';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';
import type { FieldIssue } from '../auth/api';

export const FieldIssues = createContext<FieldIssue[]>([]);

export function useFieldIssue(path?: string) {
  const issues = useContext(FieldIssues);
  return {
    'data-config-path': path,
    'aria-invalid': path && issues.some((issue) => issue.path === path) ? true : undefined,
    'aria-describedby':
      path && issues.some((issue) => issue.path === path) ? 'config-error-' + path : undefined,
  } as const;
}
const fields: Record<string, string> = {
  reviews: 'review',
  logo_mode: 'logoMode',
  logo_media_id: 'logo',
  favicon_media_id: 'favicon',
  menu_icons: 'menuIcons',
  portrait_media_id: 'portrait',
  banner_media_id: 'banner',
  public_email: 'publicEmail',
  updated_month: 'updatedMonth',
  about_key: 'aboutKey',
  main_action: 'mainAction',
  now: 'now',
  links: 'links',
  experience: 'experience',
};

export function ValidationIssues({
  issues,
  scope,
}: {
  issues: FieldIssue[];
  scope?: RefObject<HTMLElement | null>;
}) {
  const { t } = useI18n();
  const summary = useRef<HTMLElement>(null);
  useEffect(() => {
    if (issues.length) {
      summary.current?.focus();
      summary.current?.scrollIntoView?.({ block: 'start' });
    }
  }, [issues]);
  if (!issues.length) {
    return null;
  }
  function fieldName(path: string) {
    if (path.startsWith('security.')) {
      const key = (
        { label: 'keyLabel', password: 'newPassword', passkeys: 'keys', code: 'code' } as Record<
          string,
          string
        >
      )[path.split('.')[1]];
      const token = (
        {
          token_label: 'label',
          token_collections: 'collections',
          token_scopes: 'title',
          token_days: 'title',
        } as Record<string, string>
      )[path.split('.')[1]];
      return token ? t('publisher.' + token) : t('auth.' + (key || 'security'));
    }
    const parts = path.split('.');
    const root = parts[0];
    if (root === 'menu_icons') {
      return t('config.menuIcons') + ' · ' + t('slot.' + parts[1]);
    }
    const nested = /^(now|links|experience)$/.test(root) && /^\d+$/.test(parts[1] || '');
    const labels = [t('config.' + (fields[root] || root))];
    if (nested) {
      labels.push(t('config.row', { n: Number(parts[1]) + 1 }));
    }
    const sub = parts[nested ? 2 : 1];
    if (sub && !['en', 'ar', 'fr'].includes(sub)) {
      const key =
        sub === 'label'
          ? root === 'now'
            ? 'rowLabel'
            : 'linkLabel'
          : sub === 'text'
            ? 'rowText'
            : sub;
      labels.push(sub === 'id' ? t('validation.rowIdentity') : t('config.' + key));
    }
    const lang = parts.at(-1);
    if (lang && ['en', 'ar', 'fr'].includes(lang)) {
      labels.push(t('language.' + lang));
    }
    return labels.join(' · ');
  }
  const codes = new Set([
    'text',
    'english_source',
    'language',
    'limit',
    'range',
    'month',
    'email',
    'reference',
    'id',
    'required',
    'url',
    'icon',
    'date_order',
    'image_alt',
    'media',
    'colour',
    'contrast',
    'logo_mode',
    'logo_image',
    'wordmark',
    'translation_review',
    'monogram',
    'passkey_label',
    'request_confirmation',
    'otp_invalid',
    'enrollment_expired',
    'password_length',
    'last_factor',
    'tested_passkey',
    'token_scope',
    'token_collection',
    'token_label',
    'token_days',
  ]);
  function reason(issue: FieldIssue) {
    const root = issue.path.split('.')[0];
    const limits: Record<string, number> = {
      name: 100,
      headline: 200,
      bio: 320,
      sticker: 40,
      portrait_alt: 200,
      banner_alt: 200,
      location: 120,
      copyright: 100,
      footer: 200,
    };
    let n = limits[root] || 200;
    if (root === 'now') {
      n = issue.path.includes('.label') ? 80 : 160;
    }
    if (root === 'links' || root === 'main_action') {
      n = 80;
    }
    if (root === 'experience') {
      n = issue.path.includes('.description') ? 500 : 120;
    }
    if (issue.code === 'limit') {
      n = ({ now: 3, links: 10, experience: 20 } as Record<string, number>)[root] || 20;
    }
    if (issue.code === 'media') {
      n = root === 'portrait_media_id' ? 2 : root === 'banner_media_id' ? 10 : 1;
    }
    const code =
      issue.code === 'media' && root === 'portrait_media_id'
        ? 'portrait_media'
        : codes.has(issue.code)
          ? issue.code
          : 'text';
    return t('validation.' + code, {
      n,
      min: root.startsWith('banner_') ? 0 : 1,
      max: root.startsWith('banner_') ? 100 : 20,
    });
  }
  return (
    <section
      ref={summary}
      tabIndex={-1}
      className="workspace-alert config-validation"
      data-tone="error"
      role="alert"
    >
      <h2>{t('validation.title')}</h2>
      <ul>
        {issues.map((issue, i) => (
          <li id={'config-error-' + issue.path} key={issue.path + i}>
            <button
              type="button"
              className="config-field-link"
              onClick={() => {
                const control = Array.from(
                  (scope?.current || document).querySelectorAll<HTMLElement>('[data-config-path]'),
                ).find((el) => el.dataset.configPath === issue.path);
                control?.scrollIntoView({ block: 'center' });
                if (control?.matches('input, textarea, select, button')) {
                  control.focus();
                } else {
                  control?.querySelector<HTMLElement>('input, select, button')?.focus();
                }
              }}
            >
              {fieldName(issue.path)}
            </button>
            : {reason(issue)}
          </li>
        ))}
      </ul>
    </section>
  );
}
