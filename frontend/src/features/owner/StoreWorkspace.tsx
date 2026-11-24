import { useRef, useState, type FormEvent, type ReactNode } from 'react';
import { Button, Field, useI18n, type Locale } from '@ui';
import { isLocale, localeCodes } from '../../../../ui/src/i18n/locales';
import type {
  StockAdjustment,
  StoreCategory,
  StoreDraft,
  StoreDraftInput,
  StoreImage,
  StoreSettings,
  StoreVariant,
  StoreVariantInput,
  StoreWorkspace as WorkspaceData,
} from '../../api/store';
import { AuthError, call } from '../auth/api';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { OwnerPageHeading } from './OwnerWorkspace';

interface Access {
  csrf: string;
  root: string;
}

interface SaveForm {
  submit: () => Promise<void>;
  title: string;
  children: ReactNode;
}

function SubmissionForm({ submit, title, children }: SaveForm) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');

  async function save(event: FormEvent) {
    event.preventDefault();
    if (busy) {
      return;
    }
    setBusy(true);
    setNotice('');
    try {
      await submit();
      setNotice('store.saved');
    } catch (error) {
      const code = error instanceof AuthError ? error.code : '';
      setNotice(
        code === 'store_stock'
          ? 'store.stockError'
          : code === 'store_variant'
            ? 'store.variantRequired'
            : code === 'revision_conflict'
              ? 'store.conflict'
              : 'store.saveFailed',
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="owner-panel owner-form" onSubmit={(event) => void save(event)}>
      <h3>{title}</h3>
      <fieldset className="owner-fields" disabled={busy}>
        {children}
      </fieldset>
      {notice && <p role={notice === 'store.saved' ? 'status' : 'alert'}>{t(notice)}</p>}
    </form>
  );
}

export function StoreWorkspace({ csrf, staff = false }: { csrf: string; staff?: boolean }) {
  const { locale, t } = useI18n();
  const access = { csrf, root: staff ? '/staff/store' : '/owner/store' };
  const [language, setLanguage] = useState(locale);
  const [cursor, setCursor] = useState('');
  const [key, setKey] = useState('');
  const [newKey, setNewKey] = useState('');
  const workspace = useResource<WorkspaceData>(
    '/api/v1' + access.root + '?' + new URLSearchParams({ locale: language, cursor }),
    false,
    false,
    true,
  );
  const productPath = access.root + '/products/' + encodeURIComponent(key) + '/' + language;
  const product = useResource<StoreDraft>(
    key ? '/api/v1' + productPath : undefined,
    false,
    false,
    true,
  );
  const [draft, setDraft] = useState<StoreDraft>();

  function select(next: string) {
    setDraft(undefined);
    setKey(next);
  }

  function refresh(saved: StoreDraft) {
    setDraft(saved);
    setKey(saved.key);
    workspace.retry();
  }

  const selected = draft && draft.key === key && draft.locale === language ? draft : product.data;

  return (
    <section className="store-workspace">
      {staff ? (
        <h2>{t('owner.store')}</h2>
      ) : (
        <OwnerPageHeading title={t('owner.store')} description={t('store.help')} />
      )}
      <Field label={t('store.language')}>
        <select
          value={language}
          onChange={(event) => {
            if (isLocale(event.target.value)) {
              setLanguage(event.target.value);
              setDraft(undefined);
              setCursor('');
            }
          }}
        >
          {localeCodes.map((code) => (
            <option value={code} key={code}>
              {t('language.' + code)}
            </option>
          ))}
        </select>
      </Field>
      <ResourceState resource={workspace} />
      {workspace.data && (
        <>
          {!staff && (
            <SettingsEditor
              key={workspace.data.settings.version}
              access={access}
              data={workspace.data.settings}
              refresh={workspace.retry}
            />
          )}
          <CategoryEditor
            key={language}
            access={access}
            language={language}
            categories={workspace.data.categories}
            refresh={workspace.retry}
          />
          <section className="owner-panel owner-form">
            <h3>{t('store.products')}</h3>
            {workspace.data.products.length === 0 && <p>{t('store.empty')}</p>}
            <ul className="store-product-list">
              {workspace.data.products.map((product) => (
                <li key={product.key}>
                  <Button
                    variant={key === product.key ? 'secondary' : 'quiet'}
                    onClick={() => select(product.key)}
                  >
                    <bdi>{product.title}</bdi>
                  </Button>{' '}
                  <span>
                    {t(
                      product.published
                        ? 'owner.published'
                        : product.localized
                          ? 'owner.draft'
                          : 'store.missingLocale',
                    )}
                  </span>
                </li>
              ))}
            </ul>
            <div className="owner-row">
              {cursor && <Button onClick={() => setCursor('')}>{t('staff.first')}</Button>}
              {workspace.data.next && (
                <Button onClick={() => setCursor(workspace.data?.next ?? '')}>
                  {t('public.next')}
                </Button>
              )}
            </div>
            <Field label={t('store.productKey')}>
              <input
                dir="ltr"
                maxLength={64}
                value={newKey}
                onChange={(event) => setNewKey(event.target.value)}
              />
            </Field>
            <Button
              disabled={!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(newKey)}
              onClick={() => {
                setKey(newKey);
                setDraft({
                  key: newKey,
                  locale: language,
                  version: 0,
                  slug: newKey,
                  title: '',
                  description: '',
                  category: '',
                  images: [],
                  variants: [],
                  published: false,
                  published_version: 0,
                });
                setNewKey('');
              }}
            >
              {t('store.newProduct')}
            </Button>
          </section>
          {!selected && <ResourceState resource={product} />}
          {selected && (
            <>
              <DraftEditor
                key={selected.key + ':' + language + ':' + selected.version}
                access={access}
                product={selected}
                categories={workspace.data.categories}
                path={productPath}
                saved={refresh}
              />
              {selected.version > 0 && (
                <VariantsEditor
                  key={selected.key + ':' + language}
                  access={access}
                  product={selected}
                  currency={workspace.data.settings.currency}
                  saved={(value) =>
                    setDraft({
                      ...selected,
                      variants: selected.variants.some((item) => item.sku === value.sku)
                        ? selected.variants.map((item) => (item.sku === value.sku ? value : item))
                        : [...selected.variants, value],
                    })
                  }
                />
              )}
            </>
          )}
        </>
      )}
    </section>
  );
}

function SettingsEditor({
  access,
  data,
  refresh,
}: {
  access: Access;
  data: StoreSettings;
  refresh: () => void;
}) {
  const { locale, t } = useI18n();
  const [enabled, setEnabled] = useState(data.enabled);
  const [currency, setCurrency] = useState(data.currency);
  return (
    <SubmissionForm
      title={t('store.settings')}
      submit={async () => {
        await call(
          access.root + '/settings',
          'PUT',
          { version: data.version, enabled, currency },
          access.csrf,
        );
        refresh();
      }}
    >
      <label>
        <input
          type="checkbox"
          checked={enabled}
          onChange={(event) => setEnabled(event.target.checked)}
        />{' '}
        {t('store.enable')}
      </label>
      <Field label={t('store.currency')} help={t('store.currencyHelp')}>
        <input
          required
          dir="ltr"
          pattern="[A-Z]{3}"
          maxLength={3}
          value={currency}
          onChange={(event) => setCurrency(event.target.value.toUpperCase())}
        />
      </Field>
      <div className="owner-row">
        <Button type="submit" variant="primary">
          {t('store.saveSettings')}
        </Button>
        {data.enabled && <Button href={`/${locale}/store`}>{t('store.view')}</Button>}
      </div>
    </SubmissionForm>
  );
}

function CategoryEditor({
  access,
  language,
  categories,
  refresh,
}: {
  access: Access;
  language: Locale;
  categories: StoreCategory[];
  refresh: () => void;
}) {
  const { t } = useI18n();
  const [picked, setPicked] = useState('');
  const [key, setKey] = useState('');
  const [title, setTitle] = useState('');
  const selected = categories.find((item) => item.key === picked);
  return (
    <SubmissionForm
      title={t('store.categories')}
      submit={async () => {
        const saved = await call<StoreCategory>(
          access.root + '/categories',
          'PUT',
          { key: selected?.key ?? key, locale: language, version: selected?.version ?? 0, title },
          access.csrf,
        );
        setPicked(saved.key);
        refresh();
      }}
    >
      <Field label={t('public.category')}>
        <select
          value={picked}
          onChange={(event) => {
            setPicked(event.target.value);
            setTitle(categories.find((item) => item.key === event.target.value)?.title ?? '');
          }}
        >
          <option value="">{t('store.newCategory')}</option>
          {categories.map((item) => (
            <option key={item.key} value={item.key}>
              {item.title}
            </option>
          ))}
        </select>
      </Field>
      {!selected && (
        <Field label={t('store.categoryKey')}>
          <input
            dir="ltr"
            required
            maxLength={64}
            value={key}
            onChange={(event) => setKey(event.target.value)}
          />
        </Field>
      )}
      <Field label={t('owner.title')}>
        <input
          dir="auto"
          required
          maxLength={120}
          value={title}
          onChange={(event) => setTitle(event.target.value)}
        />
      </Field>
      <Button type="submit">{t('store.saveCategory')}</Button>
    </SubmissionForm>
  );
}

function DraftEditor({
  access,
  product,
  categories,
  path,
  saved,
}: {
  access: Access;
  product: StoreDraft;
  categories: StoreCategory[];
  path: string;
  saved: (product: StoreDraft) => void;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<StoreDraftInput>(() => ({
    version: product.version,
    slug: product.slug,
    title: product.title,
    description: product.description,
    category: product.category,
    images: product.images,
  }));
  const [file, setFile] = useState<File>();
  const [uploading, setUploading] = useState(false);
  const [publishBusy, setPublishBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const uploadKey = useRef('');
  const dirty =
    ['slug', 'title', 'description', 'category'].some(
      (field) => draft[field as keyof StoreDraftInput] !== product[field as keyof StoreDraftInput],
    ) || JSON.stringify(draft.images) !== JSON.stringify(product.images);
  function update(field: 'slug' | 'title' | 'description' | 'category', value: string) {
    setDraft((old) => ({ ...old, [field]: value }));
  }

  async function upload() {
    if (!file || uploading || draft.images.length >= 8) {
      return;
    }
    setUploading(true);
    setNotice('');
    try {
      const body = new FormData();
      body.append('file', file);
      const response = await fetch('/api/v1' + access.root + '/images', {
        method: 'POST',
        credentials: 'same-origin',
        body,
        headers: { 'X-CSRF-Token': access.csrf, 'Idempotency-Key': uploadKey.current },
      });
      if (!response.ok) {
        throw new Error('upload failed');
      }
      const { id } = (await response.json()) as StoreImage;
      setDraft((old) => ({ ...old, images: [...old.images, { id, alt: '' }] }));
      setFile(undefined);
    } catch {
      setNotice('store.imageFailed');
    } finally {
      setUploading(false);
    }
  }

  async function publish(published: boolean) {
    if (publishBusy || dirty) {
      return;
    }
    setPublishBusy(true);
    setNotice('');
    try {
      saved(
        await call<StoreDraft>(
          path + '/publication',
          'PUT',
          { version: product.version, published },
          access.csrf,
        ),
      );
    } catch (error) {
      setNotice(
        error instanceof AuthError && error.code === 'store_variant'
          ? 'store.variantRequired'
          : 'store.saveFailed',
      );
    } finally {
      setPublishBusy(false);
    }
  }

  return (
    <SubmissionForm
      title={product.title || t('store.newProduct')}
      submit={async () => {
        saved(await call<StoreDraft>(path, 'PUT', draft, access.csrf));
      }}
    >
      <Field label={t('owner.title')}>
        <input
          required
          maxLength={160}
          dir="auto"
          value={draft.title}
          onChange={(event) => update('title', event.target.value)}
        />
      </Field>
      <Field label={t('owner.slug')}>
        <input
          required
          maxLength={160}
          dir="auto"
          value={draft.slug}
          onChange={(event) => update('slug', event.target.value)}
        />
      </Field>
      <Field label={t('store.description')}>
        <textarea
          required
          maxLength={4096}
          dir="auto"
          rows={5}
          value={draft.description}
          onChange={(event) => update('description', event.target.value)}
        />
      </Field>
      <Field label={t('public.category')}>
        <select value={draft.category} onChange={(event) => update('category', event.target.value)}>
          <option value="">{t('owner.none')}</option>
          {categories.map((item) => (
            <option key={item.key} value={item.key}>
              {item.title}
            </option>
          ))}
        </select>
      </Field>
      <div className="store-admin-gallery">
        {draft.images.map((image) => (
          <div className="vb-card" key={image.id}>
            <img src={'/api/v1' + access.root + '/images/' + image.id} alt={image.alt} />
            <Field label={t('store.imageAlt')}>
              <input
                maxLength={300}
                required
                value={image.alt}
                dir="auto"
                onChange={(event) => {
                  setDraft((old) => ({
                    ...old,
                    images: old.images.map((item) =>
                      item.id === image.id ? { ...item, alt: event.target.value } : item,
                    ),
                  }));
                }}
              />
            </Field>
            <Button
              onClick={() =>
                setDraft((old) => ({
                  ...old,
                  images: old.images.filter((item) => item.id !== image.id),
                }))
              }
            >
              {t('store.removeImage')}
            </Button>
          </div>
        ))}
      </div>
      {draft.images.length < 8 && (
        <div className="owner-row">
          <Field label={t('store.image')}>
            <input
              type="file"
              accept="image/*"
              disabled={uploading}
              onChange={(event) => {
                setFile(event.target.files?.[0]);
                uploadKey.current = crypto.randomUUID().replaceAll('-', '');
              }}
            />
          </Field>
          <Button disabled={!file || uploading} onClick={() => void upload()}>
            {t('store.uploadImage')}
          </Button>
        </div>
      )}
      <div className="owner-row">
        <Button type="submit" variant="primary" disabled={uploading || publishBusy}>
          {t('owner.save')}
        </Button>
        {product.version > 0 && (
          <Button
            onClick={() => void publish(true)}
            disabled={
              dirty || uploading || publishBusy || product.version === product.published_version
            }
          >
            {t('owner.publish')}
          </Button>
        )}
        {product.published && (
          <Button onClick={() => void publish(false)} disabled={dirty || uploading || publishBusy}>
            {t('owner.unpublish')}
          </Button>
        )}
        {product.public_url && <Button href={product.public_url}>{t('store.view')}</Button>}
      </div>
      {product.version > 0 && dirty && <p>{t('store.saveToPublish')}</p>}
      {notice && <p role="alert">{t(notice)}</p>}
    </SubmissionForm>
  );
}

function VariantsEditor({
  access,
  product,
  currency,
  saved,
}: {
  access: Access;
  product: StoreDraft;
  currency: string;
  saved: (value: StoreVariant) => void;
}) {
  const { t } = useI18n();
  const [selected, setSelected] = useState('');
  const variant = product.variants.find((item) => item.sku === selected);
  return (
    <section className="owner-fields">
      <Field label={t('store.variants')}>
        <select value={selected} onChange={(event) => setSelected(event.target.value)}>
          <option value="">{t('store.newVariant')}</option>
          {product.variants.map((item) => (
            <option key={item.sku} value={item.sku}>
              {item.sku}
            </option>
          ))}
        </select>
      </Field>
      <VariantEditor
        key={selected + ':' + (variant?.version ?? 0)}
        access={access}
        item={variant}
        product={product}
        currency={currency}
        saved={(value) => {
          setSelected(value.sku);
          saved(value);
        }}
      />
      {variant && (
        <StockEditor
          key={variant.sku}
          access={access}
          item={variant}
          applied={(stock) => saved({ ...variant, stock })}
        />
      )}
    </section>
  );
}

function VariantEditor({
  access,
  item,
  product,
  currency,
  saved,
}: {
  access: Access;
  item?: StoreVariant;
  product: StoreDraft;
  currency: string;
  saved: (value: StoreVariant) => void;
}) {
  const { t } = useI18n();
  const [sku, setSKU] = useState(item?.sku ?? '');
  const [price, setPrice] = useState(item ? String(item.price_minor) : '');
  const [label, setLabel] = useState(item?.labels[product.locale] ?? '');
  const [active, setActive] = useState(item?.active ?? true);
  return (
    <SubmissionForm
      title={t(item ? 'store.variant' : 'store.newVariant')}
      submit={async () => {
        const labels = { ...item?.labels };
        if (label.trim()) {
          labels[product.locale] = label.trim();
        } else {
          delete labels[product.locale];
        }
        const body: StoreVariantInput = {
          version: item?.version ?? 0,
          active,
          price_minor: Number(price),
          labels,
        };
        saved(
          await call<StoreVariant>(
            access.root + '/products/' + product.key + '/variants/' + encodeURIComponent(sku),
            'PUT',
            body,
            access.csrf,
          ),
        );
      }}
    >
      <Field label={t('store.sku')}>
        <input
          readOnly={!!item}
          required
          dir="ltr"
          maxLength={48}
          value={sku}
          onChange={(event) => setSKU(event.target.value.toUpperCase())}
        />
      </Field>
      <Field label={t('store.variantLabel')}>
        <input
          dir="auto"
          maxLength={120}
          value={label}
          onChange={(event) => setLabel(event.target.value)}
        />
      </Field>
      <Field label={t('store.price', { currency })}>
        <input
          required
          type="number"
          min={1}
          max={1_000_000_000_000}
          step={1}
          value={price}
          onChange={(event) => setPrice(event.target.value)}
        />
      </Field>
      <label>
        <input
          type="checkbox"
          checked={active}
          onChange={(event) => setActive(event.target.checked)}
        />{' '}
        {t('store.active')}
      </label>
      <Button type="submit">{t('store.saveVariant')}</Button>
    </SubmissionForm>
  );
}

function StockEditor({
  access,
  item,
  applied,
}: {
  access: Access;
  item: StoreVariant;
  applied: (stock: number) => void;
}) {
  const { t, date } = useI18n();
  const path = access.root + '/inventory/' + item.sku;
  const history = useResource<StockAdjustment[]>('/api/v1' + path);
  const [delta, setDelta] = useState('');
  const [reason, setReason] = useState('');
  const pending = useRef<{ signature: string; id: string } | undefined>(undefined);
  return (
    <SubmissionForm
      title={t('store.inventory')}
      submit={async () => {
        const change = { delta: Number(delta), reason: reason.trim() };
        const signature = JSON.stringify(change);
        const request =
          pending.current?.signature === signature
            ? pending.current
            : { signature, id: crypto.randomUUID().replaceAll('-', '') };
        pending.current = request;
        const saved = await call<StockAdjustment>(
          path,
          'POST',
          { ...change, id: request.id },
          access.csrf,
        );
        pending.current = undefined;
        setDelta('');
        setReason('');
        history.retry();
        applied(saved.after);
      }}
    >
      <p>{t('store.quantity', { quantity: item.stock })}</p>
      <Field label={t('store.delta')}>
        <input
          required
          type="number"
          min={-1_000_000_000}
          max={1_000_000_000}
          step={1}
          value={delta}
          onChange={(event) => setDelta(event.target.value)}
        />
      </Field>
      <Field label={t('store.reason')}>
        <input
          required
          maxLength={300}
          dir="auto"
          value={reason}
          onChange={(event) => setReason(event.target.value)}
        />
      </Field>
      <Button type="submit" disabled={Number(delta) === 0}>
        {t('store.adjust')}
      </Button>
      <h4>{t('store.history')}</h4>
      <ResourceState resource={history} />
      <ol className="store-adjustments">
        {history.data?.map((entry) => (
          <li key={entry.id}>
            <span>{date(entry.created_at, { dateStyle: 'medium', timeStyle: 'short' })}</span>
            {' · '}
            <bdi>
              {entry.before} → {entry.after}
            </bdi>
            {' · '}
            <bdi>{entry.reason}</bdi>
            {' · '}
            <bdi>{entry.actor}</bdi>
          </li>
        ))}
      </ol>
    </SubmissionForm>
  );
}
