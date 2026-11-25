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
  event_id: string;
  recovered: boolean;
  review_note?: string;
  reviewed_at?: number;
}

interface Recovery {
  cutoff: number;
  target: number;
  pending: number;
  unresolved: number;
  uncertain: number;
  can_confirm: boolean;
  connections: {
    id: string;
    mode: 'sandbox' | 'live';
    connected: boolean;
    start: number;
    through: number;
    note?: string;
  }[];
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
  const recovery = useResource<Recovery>(
    status.data?.review_required ? '/api/v1' + root + '/recovery' : undefined,
    false,
    false,
    true,
  );
  const action = useAction();
  function refreshRecovery() {
    status.retry();
    recovery.retry();
    receipts.retry();
  }

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
              <ResourceState resource={recovery} />
              {recovery.data && (
                <RecoveryControls recovery={recovery.data} csrf={csrf} updated={refreshRecovery} />
              )}
              <div className="owner-row">
                <Button disabled={action.busy} onClick={refreshRecovery}>
                  {t('paypal.refreshRecovery')}
                </Button>
                <Button
                  disabled={action.busy || !recovery.data?.can_confirm}
                  onClick={() =>
                    void action.run(async () => {
                      if (!window.confirm(t('paypal.confirmRecoveryScope'))) {
                        return;
                      }
                      await freshPost(root + '/resume', {});
                      refreshRecovery();
                      updated();
                    })
                  }
                >
                  {t('paypal.confirmReview')}
                </Button>
              </div>
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
                      {row.recovered && <> · {t('paypal.recovered')}</>}
                    </p>
                    {row.state === 'review' && (
                      <>
                        <p>
                          <bdi>{row.event_id}</bdi>
                        </p>
                        {row.review_note && <p dir="auto">{row.review_note}</p>}
                        {!row.reviewed_at && (
                          <RecoveryNote
                            onSave={async (note) => {
                              await freshPost(root + '/receipts/' + row.id + '/review', { note });
                              refreshRecovery();
                            }}
                          />
                        )}
                        <Button
                          disabled={action.busy}
                          onClick={() =>
                            void action.run(async () => {
                              await call(root + '/receipts/' + row.id + '/retry', 'POST', {}, csrf);
                              refreshRecovery();
                            })
                          }
                        >
                          {t('paypal.retryCheck')}
                        </Button>
                      </>
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

function RecoveryControls({
  recovery,
  csrf,
  updated,
}: {
  recovery: Recovery;
  csrf: string;
  updated: () => void;
}) {
  const { locale, t } = useI18n();
  const action = useAction();
  const date = new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: 'UTC',
  });
  return (
    <section>
      <p>
        {recovery.cutoff
          ? t('paypal.recoveryCutoff', { date: date.format(recovery.cutoff * 1000) })
          : t('paypal.recoveryUnknown')}
      </p>
      {recovery.target > 0 && (
        <p>{t('paypal.recoveryTarget', { date: date.format(recovery.target * 1000) })}</p>
      )}
      <p>
        {t('paypal.recoveryCounts', {
          pending: recovery.pending,
          unresolved: recovery.unresolved,
          uncertain: recovery.uncertain,
        })}
      </p>
      <ul className="store-product-list">
        {recovery.connections.map((connection) => (
          <li key={connection.id}>
            <p>
              <bdi>{connection.id}</bdi> · {t('paypal.mode.' + connection.mode)}
            </p>
            {connection.through > 0 && (
              <p>{t('paypal.recoveryThrough', { date: date.format(connection.through * 1000) })}</p>
            )}
            {connection.note && <p dir="auto">{connection.note}</p>}
            {connection.through < recovery.target && (
              <>
                {connection.connected && recovery.cutoff > 0 && (
                  <Button
                    disabled={action.busy}
                    onClick={() =>
                      void action.run(async () => {
                        await call(root + '/recovery', 'POST', { connection: connection.id }, csrf);
                        updated();
                      })
                    }
                  >
                    {t('paypal.importHistory')}
                  </Button>
                )}
                <RecoveryNote
                  manual
                  onSave={async (note) => {
                    await freshPost(root + '/recovery/manual', { connection: connection.id, note });
                    updated();
                  }}
                />
              </>
            )}
          </li>
        ))}
      </ul>
      {action.notice && <p role="status">{t(action.notice)}</p>}
    </section>
  );
}

function RecoveryNote({
  onSave,
  manual = false,
}: {
  onSave: (note: string) => Promise<void>;
  manual?: boolean;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState('');
  const action = useAction();
  const label = t(manual ? 'paypal.manualRecovery' : 'paypal.reviewReceipt');

  return (
    <div>
      <Button aria-expanded={open} disabled={action.busy} onClick={() => setOpen(!open)}>
        {label}
      </Button>
      {open && (
        <form
          className="owner-form"
          onSubmit={(event) => {
            event.preventDefault();
            void action.run(async () => {
              await onSave(note.trim());
              setNote('');
              setOpen(false);
            });
          }}
        >
          <Field label={t('paypal.recoveryNote')}>
            <input
              maxLength={300}
              required
              disabled={action.busy}
              value={note}
              dir="auto"
              onChange={(event) => setNote(event.target.value)}
            />
          </Field>
          <Button type="submit" disabled={action.busy || !note.trim()}>
            {label}
          </Button>
        </form>
      )}
      {action.notice && <p role="status">{t(action.notice)}</p>}
    </div>
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
