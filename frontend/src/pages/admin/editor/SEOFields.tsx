import { Field, useI18n } from '@ui';
import type { DraftInput, RevisionSEO } from '../../../api/content';

export function SEOFields({
  draft,
  onChange,
}: {
  draft: DraftInput;
  onChange: (value: RevisionSEO) => void;
}) {
  const { t } = useI18n();
  const seo = draft.seo ?? {};
  return (
    <fieldset>
      <legend>{t('seo.heading')}</legend>
      <Field label={t('seo.title')}>
        <input
          maxLength={120}
          dir="auto"
          value={seo.title ?? ''}
          onChange={(event) => onChange({ ...seo, title: event.target.value })}
        />
      </Field>
      <Field label={t('seo.description')}>
        <input
          maxLength={320}
          dir="auto"
          value={seo.description ?? ''}
          onChange={(event) => onChange({ ...seo, description: event.target.value })}
        />
      </Field>
      <Field label={t('seo.image')}>
        <input
          dir="ltr"
          value={seo.social_image_id ?? ''}
          onChange={(event) => onChange({ ...seo, social_image_id: event.target.value })}
        />
      </Field>
      <Field label={t('seo.schema')}>
        <select
          value={seo.schema ?? ''}
          onChange={(event) => {
            const schema = event.target.value as RevisionSEO['schema'];
            onChange({ ...seo, schema, product: schema === 'product' ? { sku: '' } : undefined });
          }}
        >
          <option value="">{t('seo.none')}</option>
          <option value="web_page">{t('seo.webPage')}</option>
          {draft.kind === 'page' && <option value="product">{t('seo.product')}</option>}
          {draft.kind === 'article' && <option value="article">{t('slot.writing')}</option>}
        </select>
      </Field>
      {seo.schema === 'product' && (
        <>
          <Field label={t('seo.sku')}>
            <input
              required
              dir="auto"
              maxLength={80}
              value={seo.product?.sku ?? ''}
              onChange={(event) =>
                onChange({ ...seo, product: { ...seo.product, sku: event.target.value } })
              }
            />
          </Field>
          <Field label={t('seo.brand')}>
            <input
              dir="auto"
              maxLength={100}
              value={seo.product?.brand ?? ''}
              onChange={(event) =>
                onChange({
                  ...seo,
                  product: { sku: seo.product?.sku ?? '', brand: event.target.value },
                })
              }
            />
          </Field>
        </>
      )}
      <label className="config-check">
        <input
          type="checkbox"
          checked={seo.no_index ?? false}
          onChange={(event) => onChange({ ...seo, no_index: event.target.checked })}
        />
        {t('seo.noIndex')}
      </label>
      <div>
        <h3>{t('seo.preview')}</h3>
        <p dir="auto">
          <strong>{seo.title || draft.title}</strong>
        </p>
        <p dir="auto">{seo.description || draft.summary}</p>
      </div>
    </fieldset>
  );
}
