import { useI18n } from '@ui';
import type { OrderStatus, Snapshot } from '../../api/checkout';
import { AuthError, requestErrorKey } from '../auth/api';

export function checkoutError(error: unknown) {
  if (error instanceof AuthError) {
    const labels: Record<string, string> = {
      store_coupon: 'checkout.couponInvalid',
      store_payment_amount: 'paypal.badAmount',
      store_order_state: 'order.stateConflict',
      paypal_review: 'paypal.needsReview',
      store_shipping: 'checkout.shippingInvalid',
      store_stock: 'checkout.stockInvalid',
      store_quote_changed: 'checkout.quoteChanged',
      idempotency_conflict: 'checkout.alreadyUsed',
    };
    if (labels[error.code]) {
      return labels[error.code];
    }
  }
  return requestErrorKey(error);
}

export function OrderTotals({ data }: { data: Snapshot }) {
  const { locale, t } = useI18n();
  const money = new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: data.currency,
    minimumFractionDigits: data.digits,
    maximumFractionDigits: data.digits,
  });
  const format = (minor: number) => money.format(minor / 10 ** data.digits);
  const totals = [
    ['checkout.subtotal', data.subtotal_minor],
    ['checkout.discount', -data.discount_minor || 0],
    ['checkout.shipping', data.shipping_minor],
    ['checkout.tax', data.tax_minor],
    ['checkout.total', data.total_minor],
  ] as const;
  return (
    <div>
      <ul>
        {data.lines.map((line) => (
          <li key={line.sku}>
            <bdi>{line.title}</bdi> · <bdi>{line.variant}</bdi> × {line.quantity}{' '}
            <bdi>{format(line.line_minor)}</bdi>
          </li>
        ))}
      </ul>
      <dl className="store-order-totals">
        {totals.map(([label, value]) => (
          <div key={label}>
            <dt>{t(label)}</dt>
            <dd>
              <bdi>{format(value)}</bdi>
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

export function OrderSummary({ order }: { order: OrderStatus }) {
  const { locale, t } = useI18n();
  const date = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' });
  return (
    <div>
      <p>
        {t('checkout.reference')} <bdi>{order.reference}</bdi>
      </p>
      <dl className="store-order-totals">
        <div>
          <dt>{t('checkout.orderState')}</dt>
          <dd>{t('checkout.state.' + order.status)}</dd>
        </div>
        <div>
          <dt>{t('checkout.paymentState')}</dt>
          <dd>{t('checkout.state.' + order.payment)}</dd>
        </div>
        <div>
          <dt>{t('checkout.refundState')}</dt>
          <dd>{t('checkout.state.' + order.refund)}</dd>
        </div>
        <div>
          <dt>{t('checkout.fulfillmentState')}</dt>
          <dd>{t('checkout.state.' + order.fulfillment)}</dd>
        </div>
        {order.status === 'awaiting_payment' && (
          <div>
            <dt>{t('checkout.holdUntil')}</dt>
            <dd>{date.format(order.expires_at * 1000)}</dd>
          </div>
        )}
      </dl>
      {order.status === 'awaiting_payment' && <p>{t('checkout.reservationOnly')}</p>}
      {order.shipment && (
        <dl className="store-order-totals">
          <div>
            <dt>{t('order.shippedAt')}</dt>
            <dd>{date.format(order.shipment.shipped_at * 1000)}</dd>
          </div>
          {order.shipment.carrier && (
            <div>
              <dt>{t('order.carrier')}</dt>
              <dd>
                <bdi>{order.shipment.carrier}</bdi>
              </dd>
            </div>
          )}
          {order.shipment.tracking_number && (
            <div>
              <dt>{t('order.tracking')}</dt>
              <dd>
                <bdi>{order.shipment.tracking_number}</bdi>
              </dd>
            </div>
          )}
        </dl>
      )}
      {order.notices.length > 0 && (
        <section aria-label={t('order.notices')}>
          <h4>{t('order.notices')}</h4>
          <ul>
            {order.notices.map((notice) => (
              <li key={notice.kind}>
                {t('order.notice.' + notice.kind)} · {date.format(notice.created_at * 1000)}
              </li>
            ))}
          </ul>
        </section>
      )}
      {order.refunded_minor > 0 && (
        <p>
          {t('order.refundedAmount')}{' '}
          <bdi>
            {new Intl.NumberFormat(locale, {
              style: 'currency',
              currency: order.snapshot.currency,
              minimumFractionDigits: order.snapshot.digits,
              maximumFractionDigits: order.snapshot.digits,
            }).format(order.refunded_minor / 10 ** order.snapshot.digits)}
          </bdi>
        </p>
      )}
      <OrderTotals data={order.snapshot} />
    </div>
  );
}
