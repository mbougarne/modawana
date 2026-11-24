import { useState, type FormEvent } from 'react';
import { Button, Field, useI18n } from '@ui';
import type {
  CheckoutSettings,
  Coupon,
  MerchantOrder,
  Orders,
  ShippingZone,
} from '../../api/checkout';
import { call } from '../auth/api';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { checkoutError, OrderSummary } from './OrderSummary';
import { PayPalSettings, PayPalOrderTools } from './PayPalAdmin';
import { MerchantOperations, MerchantHistory } from './MerchantOperations';
import './checkout.css';

interface Access {
  csrf: string;
  root: string;
}

export function CheckoutAdmin({ access }: { access: Access }) {
  const settings = useResource<CheckoutSettings>(
    '/api/v1' + access.root + '/checkout',
    false,
    false,
    true,
  );
  return (
    <>
      <PayPalSettings csrf={access.csrf} updated={settings.retry} />
      <ResourceState resource={settings} />
      {settings.data && (
        <SettingsForm
          key={settings.data.version}
          access={access}
          initial={settings.data}
          saved={settings.retry}
        />
      )}
    </>
  );
}

function SettingsForm({
  initial,
  access,
  saved,
}: {
  initial: CheckoutSettings;
  access: Access;
  saved: () => void;
}) {
  const { t } = useI18n();
  const [enabled, setEnabled] = useState(initial.enabled);
  const [zones, setZones] = useState(initial.zones);
  const [coupons, setCoupons] = useState(initial.coupons);
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) {
      return;
    }
    setBusy(true);
    setNotice('');
    try {
      await call(
        access.root + '/checkout',
        'PUT',
        {
          version: initial.version,
          enabled,
          zones: zones.map((zone) => ({
            ...zone,
            countries: zone.countries
              .map((country) => country.trim().toUpperCase())
              .filter(Boolean),
          })),
          coupons: coupons.map((coupon) => ({ ...coupon, code: coupon.code.trim().toUpperCase() })),
        },
        access.csrf,
      );
      saved();
    } catch (error) {
      setNotice(checkoutError(error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="owner-panel owner-form" onSubmit={(event) => void submit(event)}>
      <h3>{t('checkout.settings')}</h3>
      <fieldset disabled={busy} className="owner-fields">
        <label>
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />{' '}
          {t('checkout.enable')}
        </label>
        <p>{t('checkout.settingsHelp')}</p>
        <h4>{t('checkout.zones')}</h4>
        {zones.map((zone, index) => (
          <ZoneEditor
            key={index}
            zone={zone}
            change={(edited) => setZones(zones.map((item, i) => (i === index ? edited : item)))}
            remove={() => setZones(zones.filter((_, i) => i !== index))}
          />
        ))}
        <Button
          disabled={zones.length >= 32}
          onClick={() =>
            setZones([
              ...zones,
              { name: '', countries: [], rate_minor: 0, tax_bps: 0, tax_shipping: false },
            ])
          }
        >
          {t('checkout.addZone')}
        </Button>
        <h4>{t('checkout.coupons')}</h4>
        {coupons.map((coupon, index) => (
          <CouponEditor
            key={index}
            coupon={coupon}
            change={(edited) => setCoupons(coupons.map((item, i) => (i === index ? edited : item)))}
            remove={() => setCoupons(coupons.filter((_, i) => i !== index))}
          />
        ))}
        <Button
          disabled={coupons.length >= 64}
          onClick={() => setCoupons([...coupons, { code: '', discount_minor: 0, discount_bps: 0 }])}
        >
          {t('checkout.addCoupon')}
        </Button>
        <Button type="submit">{t('store.saveSettings')}</Button>
      </fieldset>
      {notice && <p role="alert">{t(notice)}</p>}
    </form>
  );
}

function ZoneEditor({
  zone,
  change,
  remove,
}: {
  zone: ShippingZone;
  change: (value: ShippingZone) => void;
  remove: () => void;
}) {
  const { t } = useI18n();
  return (
    <fieldset className="owner-fields">
      <Field label={t('checkout.zoneName')}>
        <input
          maxLength={80}
          required
          value={zone.name}
          dir="auto"
          onChange={(event) => change({ ...zone, name: event.target.value })}
        />
      </Field>
      <Field label={t('checkout.zoneCountries')}>
        <input
          required
          value={zone.countries.join(',')}
          dir="ltr"
          maxLength={1000}
          onChange={(event) => change({ ...zone, countries: event.target.value.split(',') })}
        />
      </Field>
      <Field label={t('checkout.shippingMinor')}>
        <input
          type="number"
          min={0}
          max={1_000_000_000_000}
          required
          value={zone.rate_minor}
          onChange={(event) => change({ ...zone, rate_minor: Number(event.target.value) })}
        />
      </Field>
      <Field label={t('checkout.taxBPS')}>
        <input
          type="number"
          min={0}
          max={10000}
          required
          value={zone.tax_bps}
          onChange={(event) => change({ ...zone, tax_bps: Number(event.target.value) })}
        />
      </Field>
      <label>
        <input
          type="checkbox"
          checked={zone.tax_shipping}
          onChange={(event) => change({ ...zone, tax_shipping: event.target.checked })}
        />{' '}
        {t('checkout.taxShipping')}
      </label>
      <Button onClick={remove}>{t('checkout.removeZone')}</Button>
    </fieldset>
  );
}

function CouponEditor({
  coupon,
  change,
  remove,
}: {
  coupon: Coupon;
  change: (value: Coupon) => void;
  remove: () => void;
}) {
  const { t } = useI18n();
  return (
    <fieldset className="owner-fields">
      <Field label={t('checkout.coupon')}>
        <input
          maxLength={32}
          required
          value={coupon.code}
          dir="ltr"
          onChange={(event) => change({ ...coupon, code: event.target.value })}
        />
      </Field>
      <Field label={t('checkout.discountMinor')}>
        <input
          type="number"
          min={0}
          max={1_000_000_000_000}
          required
          value={coupon.discount_minor}
          onChange={(event) =>
            change({ ...coupon, discount_minor: Number(event.target.value), discount_bps: 0 })
          }
        />
      </Field>
      <Field label={t('checkout.discountBPS')}>
        <input
          type="number"
          min={0}
          max={10000}
          required
          value={coupon.discount_bps}
          onChange={(event) =>
            change({ ...coupon, discount_bps: Number(event.target.value), discount_minor: 0 })
          }
        />
      </Field>
      <Button onClick={remove}>{t('checkout.removeCoupon')}</Button>
    </fieldset>
  );
}

export function StoreOrders({ access }: { access: Access }) {
  const { locale, t } = useI18n();
  const [cursor, setCursor] = useState('');
  const [reference, setReference] = useState('');
  const root = '/api/v1' + access.root + '/orders';
  const orders = useResource<Orders>(
    root + '?' + new URLSearchParams({ cursor }),
    false,
    false,
    true,
  );
  const detail = useResource<MerchantOrder>(
    reference ? root + '/' + reference : undefined,
    false,
    false,
    true,
  );
  const date = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' });
  const order = detail.data;
  return (
    <section className="owner-panel owner-form">
      <h3>{t('checkout.orders')}</h3>
      <Button href={root + '/export'} download>
        {t('order.export')}
      </Button>
      <ResourceState resource={orders} />
      {orders.data && (
        <>
          {orders.data.orders.length === 0 && <p>{t('checkout.noOrders')}</p>}
          <ul className="store-product-list">
            {orders.data.orders.map((row) => (
              <li key={row.reference}>
                <Button
                  variant={row.reference === reference ? 'secondary' : 'quiet'}
                  onClick={() => setReference(row.reference)}
                >
                  <bdi>{row.reference}</bdi>
                </Button>{' '}
                {date.format(row.created_at * 1000)} · {t('checkout.state.' + row.status)}
              </li>
            ))}
          </ul>
          <div className="owner-row">
            {cursor && <Button onClick={() => setCursor('')}>{t('staff.first')}</Button>}
            {orders.data.next && (
              <Button onClick={() => setCursor(orders.data?.next ?? '')}>{t('public.next')}</Button>
            )}
          </div>
        </>
      )}
      {reference && <ResourceState resource={detail} />}
      {order && (
        <>
          <OrderSummary order={order} />
          {order.paid_at !== undefined && (
            <Button href={root + '/' + order.reference + '/receipt'}>{t('order.receipt')}</Button>
          )}
          <MerchantOperations
            root={access.root}
            csrf={access.csrf}
            order={order}
            updated={() => {
              detail.retry();
              orders.retry();
            }}
          />
          {access.root === '/owner/store' && (
            <PayPalOrderTools
              key={order.reference}
              csrf={access.csrf}
              order={order}
              updated={detail.retry}
            />
          )}
          <h4>{t('checkout.customer')}</h4>
          <address dir="auto">
            <div>{order.customer.name}</div>
            <div>{order.customer.email}</div>
            <div>{order.customer.address}</div>
            {order.customer.address_2 && <div>{order.customer.address_2}</div>}
            <div>
              {[
                order.customer.city,
                order.customer.region,
                order.customer.postal_code,
                order.snapshot.country,
              ]
                .filter(Boolean)
                .join(', ')}
            </div>
          </address>
          <MerchantHistory order={order} />
        </>
      )}
    </section>
  );
}
