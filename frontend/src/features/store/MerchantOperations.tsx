import { useRef, useState, type FormEvent } from 'react';
import { Button, Field, useI18n } from '@ui';
import type { MerchantOrder } from '../../api/checkout';
import { call } from '../auth/api';
import { checkoutError } from './OrderSummary';

type Action = 'cancel' | 'ship' | 'tracking' | 'restock';

interface ActionInput {
  reason?: string;
  carrier?: string;
  tracking_number?: string;
  sku?: string;
  quantity?: number;
}

interface Props {
  root: string;
  csrf: string;
  order: MerchantOrder;
  updated: () => void;
}

export function MerchantOperations(props: Props) {
  const { t } = useI18n();
  const { order } = props;
  const canCancel =
    order.fulfillment === 'unfulfilled' &&
    (order.status === 'awaiting_payment' || order.status === 'confirmed') &&
    (order.payment === 'pending' || order.payment === 'paid');
  const canShip =
    order.status === 'confirmed' &&
    order.fulfillment === 'unfulfilled' &&
    order.reservation === 'sold' &&
    order.payment === 'paid' &&
    order.refund !== 'refunded' &&
    order.refund !== 'pending';
  const canTrack = order.fulfillment === 'shipped' && order.shipment !== undefined;
  const canRestock =
    order.reservation === 'sold' &&
    (order.fulfillment !== 'unfulfilled' || order.refund === 'refunded') &&
    order.snapshot.lines.some((line) => line.quantity > (order.restocked[line.sku] ?? 0));
  if (!canCancel && !canShip && !canTrack && !canRestock) {
    return null;
  }

  return (
    <section className="owner-form">
      <h4>{t('order.operations')}</h4>
      {canShip && <OperationForm {...props} action="ship" />}
      {canTrack && <OperationForm {...props} action="tracking" />}
      {canCancel && <OperationForm {...props} action="cancel" />}
      {canRestock && <OperationForm {...props} action="restock" />}
    </section>
  );
}

function OperationForm({ root, csrf, order, updated, action }: Props & { action: Action }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [sku, setSKU] = useState('');
  const lock = useRef(false);
  const attempt = useRef<{ body: string; id: string }>(undefined);
  const choices = order.snapshot.lines.filter(
    (row) => row.quantity > (order.restocked[row.sku] ?? 0),
  );
  const limit =
    (choices.find((row) => row.sku === sku)?.quantity ?? 0) - (order.restocked[sku] ?? 0);
  const hasTracking = action === 'ship' || action === 'tracking';

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (lock.current) {
      return;
    }
    const fields = new FormData(event.currentTarget);
    const read = (name: string) => String(fields.get(name) ?? '').trim();
    let input: ActionInput;
    if (hasTracking) {
      input = { carrier: read('carrier'), tracking_number: read('tracking_number') };
      if (Boolean(input.carrier) !== Boolean(input.tracking_number)) {
        setNotice('order.trackingPair');
        return;
      }
    } else if (action === 'restock') {
      input = { reason: read('reason'), sku, quantity: Number(read('quantity')) };
    } else {
      input = { reason: read('reason') };
    }
    if (action === 'cancel' && !window.confirm(t('order.confirmCancel'))) {
      return;
    }
    const body = JSON.stringify(input);
    if (attempt.current?.body !== body) {
      attempt.current = { id: crypto.randomUUID().replaceAll('-', ''), body };
    }
    lock.current = true;
    setBusy(true);
    setNotice('');
    try {
      await call(
        root + '/orders/' + order.reference + '/' + action,
        'POST',
        {
          ...input,
          id: attempt.current.id,
        },
        csrf,
      );
      attempt.current = undefined;
      setNotice('order.saved');
      setOpen(false);
      updated();
    } catch (error) {
      setNotice(checkoutError(error));
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }

  return (
    <div className="owner-form">
      <Button aria-expanded={open} disabled={busy} onClick={() => setOpen(!open)}>
        {t('order.action.' + action)}
      </Button>
      {open && (
        <form className="owner-form" onSubmit={(event) => void save(event)}>
          <fieldset className="owner-fields" disabled={busy}>
            {action === 'cancel' && order.payment === 'paid' && <p>{t('order.cancelPaid')}</p>}
            {hasTracking && (
              <>
                <Field label={t('order.carrier')}>
                  <input
                    name="carrier"
                    maxLength={80}
                    dir="auto"
                    defaultValue={order.shipment?.carrier}
                  />
                </Field>
                <Field label={t('order.tracking')}>
                  <input
                    name="tracking_number"
                    maxLength={120}
                    dir="auto"
                    defaultValue={order.shipment?.tracking_number}
                  />
                </Field>
              </>
            )}
            {action === 'restock' && (
              <>
                <Field label={t('store.sku')}>
                  <select value={sku} required onChange={(event) => setSKU(event.target.value)}>
                    <option value="">{t('order.chooseSKU')}</option>
                    {choices.map((line) => (
                      <option key={line.sku} value={line.sku}>
                        {line.sku}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label={t('order.quantity')}>
                  <input
                    name="quantity"
                    type="number"
                    required
                    min={1}
                    max={limit}
                    disabled={limit < 1}
                  />
                </Field>
              </>
            )}
            {!hasTracking && (
              <Field label={t('store.reason')}>
                <input name="reason" dir="auto" maxLength={300} required />
              </Field>
            )}
            <Button type="submit" disabled={action === 'restock' && limit < 1}>
              {t('order.action.' + action)}
            </Button>
          </fieldset>
        </form>
      )}
      {notice && <p role={notice === 'order.saved' ? 'status' : 'alert'}>{t(notice)}</p>}
    </div>
  );
}

export function MerchantHistory({ order }: { order: MerchantOrder }) {
  const { t, locale } = useI18n();
  const date = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' });
  return (
    <section>
      <h4>{t('order.paymentReferences')}</h4>
      <dl className="store-order-totals">
        <div>
          <dt>{t('order.paymentOperation')}</dt>
          <dd>
            <bdi>{order.operation}</bdi>
          </dd>
        </div>
        {order.payment_record.connection && (
          <div>
            <dt>{t('order.connection')}</dt>
            <dd>
              <bdi>{order.payment_record.connection}</bdi>
            </dd>
          </div>
        )}
        {order.payment_record.provider_order && (
          <div>
            <dt>{t('order.providerOrder')}</dt>
            <dd>
              <bdi>{order.payment_record.provider_order}</bdi>
            </dd>
          </div>
        )}
        {order.payment_record.capture && (
          <div>
            <dt>{t('order.capture')}</dt>
            <dd>
              <bdi>{order.payment_record.capture}</bdi>
            </dd>
          </div>
        )}
      </dl>
      {order.refunds.length > 0 && (
        <>
          <h4>{t('order.refundReferences')}</h4>
          <ul>
            {order.refunds.map((refund) => (
              <li key={refund.id}>
                <bdi>{refund.provider_id ?? refund.id}</bdi> · {t('paypal.state.' + refund.state)}
              </li>
            ))}
          </ul>
        </>
      )}
      <h4>{t('checkout.history')}</h4>
      <ul>
        {order.events.map((event, index) => (
          <li key={index}>
            {t('checkout.event.' + event.kind)} · {date.format(event.created_at * 1000)}
            {event.actor && (
              <>
                {' '}
                · <bdi>{event.actor}</bdi>
              </>
            )}
            {event.detail?.reason && (
              <>
                {' '}
                · <bdi>{event.detail.reason}</bdi>
              </>
            )}
            {event.detail?.tracking_number && (
              <>
                {' '}
                ·{' '}
                <bdi>
                  {event.detail.carrier} {event.detail.tracking_number}
                </bdi>
              </>
            )}
            {event.detail?.sku && (
              <>
                {' '}
                · <bdi>{event.detail.sku}</bdi> × {event.detail.quantity}
              </>
            )}
            {event.detail?.refund_id && (
              <>
                {' '}
                · <bdi>{event.detail.refund_id}</bdi> ·{' '}
                {t('order.refundMinor', {
                  amount: event.detail.amount_minor ?? 0,
                  currency: order.snapshot.currency,
                })}
              </>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
