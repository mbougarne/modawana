import { useRef, useState, type FormEvent } from 'react';
import { Button, Field, useI18n } from '@ui';
import type { MerchantOrder } from '../../api/checkout';
import { AuthError, call } from '../auth/api';
import { authenticatedTransfer } from '../auth/reauthenticate';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { checkoutError } from './OrderSummary';

const root = '/owner/store/paypal';

interface Status {
  mode?: 'sandbox' | 'live';
  ready: boolean;
  review_required: boolean;
  review_receipts: number;
}

interface Receipt {
  id: number;
  connection: string;
  kind: string;
  resource: string;
  provider_order?: string;
  provider_capture?: string;
  state: 'pending' | 'applied' | 'review';
  attempts: number;
}

interface Refund {
  id: string;
  amount_minor: number;
  state: 'pending' | 'complete' | 'failed';
  created_at: number;
}

async function freshPost(path: string, body: unknown) {
  const res = await authenticatedTransfer('/api/v1' + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const envelope = await res.json().catch(() => null);
    throw new AuthError(envelope?.error?.code ?? 'internal_error');
  }
}

function useAction() {
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const lock = useRef(false);
  return {
    busy,
    notice,
    run: async (action: () => Promise<void>) => {
      if (lock.current) {
        return;
      }
      lock.current = true;
      setBusy(true);
      setNotice('');
      try {
        await action();
        setNotice('paypal.updated');
      } catch (error) {
        setNotice(checkoutError(error));
      } finally {
        lock.current = false;
        setBusy(false);
      }
    },
  };
}

export function PayPalSettings({ csrf, updated }: { csrf: string; updated: () => void }) {
  const { t } = useI18n();
  const status = useResource<Status>('/api/v1' + root, false, false, true);
  const [show, setShow] = useState(false);
  const [before, setBefore] = useState(0);
  const receipts = useResource<Receipt[]>(
    show ? '/api/v1' + root + '/receipts' + (before ? '?before=' + before : '') : undefined,
    false,
    false,
    true,
  );
  const action = useAction();
  return (
    <section className="owner-panel owner-form">
      <h3>PayPal</h3>
      <ResourceState resource={status} />
      {status.data && (
        <>
          <p>
            {t(status.data.ready ? 'paypal.connected' : 'paypal.closed')}
            {status.data.mode && ' · ' + t('paypal.mode.' + status.data.mode)}
          </p>
          {status.data.review_required && (
            <>
              <p>{t('paypal.restoreReview')}</p>
              <Button
                disabled={action.busy}
                onClick={() =>
                  void action.run(async () => {
                    if (!window.confirm(t('paypal.confirmReview'))) {
                      return;
                    }
                    await freshPost(root + '/resume', {});
                    status.retry();
                    updated();
                  })
                }
              >
                {t('paypal.confirmReview')}
              </Button>
            </>
          )}
          <Button aria-expanded={show} disabled={action.busy} onClick={() => setShow(!show)}>
            {t('paypal.receipts', { count: status.data.review_receipts })}
          </Button>
          {show && (
            <>
              <ResourceState resource={receipts} />
              {receipts.data?.length === 0 && <p>{t('paypal.noReceipts')}</p>}
              <ul className="store-product-list">
                {receipts.data?.map((row) => (
                  <li key={row.id}>
                    <p>
                      <bdi>{row.kind}</bdi> · {t('paypal.state.' + row.state)}
                    </p>
                    <p>
                      <bdi>
                        {row.connection} /{' '}
                        {row.provider_order || row.provider_capture || row.resource}
                      </bdi>
                    </p>
                    {row.state === 'review' && (
                      <Button
                        disabled={action.busy}
                        onClick={() =>
                          void action.run(async () => {
                            await call(root + '/receipts/' + row.id + '/retry', 'POST', {}, csrf);
                            receipts.retry();
                            status.retry();
                          })
                        }
                      >
                        {t('paypal.retryCheck')}
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
              <div className="owner-row">
                {before > 0 && <Button onClick={() => setBefore(0)}>{t('staff.first')}</Button>}
                {receipts.data?.length === 50 && (
                  <Button onClick={() => setBefore(receipts.data?.[49].id ?? 0)}>
                    {t('public.next')}
                  </Button>
                )}
              </div>
            </>
          )}
        </>
      )}
      {action.notice && <p role="status">{t(action.notice)}</p>}
    </section>
  );
}

export function PayPalOrderTools({
  order,
  csrf,
  updated,
}: {
  order: MerchantOrder;
  csrf: string;
  updated: () => void;
}) {
  const { t } = useI18n();
  const path = root + '/orders/' + order.reference;
  const paid = order.payment === 'paid' || order.payment === 'late_paid';
  const refunds = useResource<Refund[]>(
    paid ? '/api/v1' + path + '/refunds' : undefined,
    false,
    false,
    true,
  );
  const action = useAction();
  const [amount, setAmount] = useState('');
  const attempt = useRef<{ key: string; amount_minor: number }>(undefined);
  const remaining =
    order.snapshot.total_minor -
    order.refunds
      .filter((row) => row.state !== 'failed')
      .reduce((sum, row) => sum + row.amount_minor, 0);

  function refresh() {
    refunds.retry();
    updated();
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const minor = Number(amount);
    if (
      !Number.isSafeInteger(minor) ||
      minor <= 0 ||
      minor > remaining ||
      !window.confirm(
        t('paypal.confirmRefund', { amount: minor, currency: order.snapshot.currency }),
      )
    ) {
      return;
    }
    if (!attempt.current || attempt.current.amount_minor !== minor) {
      attempt.current = { key: crypto.randomUUID().replaceAll('-', ''), amount_minor: minor };
    }
    void action.run(async () => {
      try {
        await freshPost(path + '/refunds', attempt.current);
        attempt.current = undefined;
        setAmount('');
      } finally {
        refresh();
      }
    });
  }

  return (
    <section>
      <h4>{t('paypal.payment')}</h4>
      <Button
        disabled={action.busy}
        onClick={() =>
          void action.run(async () => {
            await call(path + '/reconcile', 'POST', {}, csrf);
            refresh();
          })
        }
      >
        {t('paypal.check')}
      </Button>
      {paid && (
        <>
          <ResourceState resource={refunds} />
          <ul className="store-product-list">
            {refunds.data?.map((row) => (
              <li key={row.id}>
                <bdi>{row.amount_minor}</bdi> · {t('paypal.state.' + row.state)}{' '}
                {row.state === 'pending' && (
                  <Button
                    disabled={action.busy}
                    onClick={() =>
                      void action.run(async () => {
                        try {
                          await freshPost(root + '/refunds/' + row.id + '/retry', {});
                        } finally {
                          refresh();
                        }
                      })
                    }
                  >
                    {t('paypal.retryRefund')}
                  </Button>
                )}
              </li>
            ))}
          </ul>
          {refunds.data && remaining > 0 && (
            <form onSubmit={submit} className="owner-form">
              <Field label={t('paypal.refundAmount', { currency: order.snapshot.currency })}>
                <input
                  type="number"
                  min={1}
                  max={remaining}
                  step={1}
                  required
                  disabled={action.busy}
                  value={amount}
                  onChange={(event) => setAmount(event.target.value)}
                />
              </Field>
              <Button type="submit" disabled={action.busy || amount === ''}>
                {t('paypal.refund')}
              </Button>
            </form>
          )}
        </>
      )}
      {action.notice && <p role="status">{t(action.notice)}</p>}
    </section>
  );
}
