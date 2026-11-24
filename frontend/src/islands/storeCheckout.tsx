import { useEffect, useRef, useState, type FormEvent } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, Field, I18nProvider, useI18n, type LabelOverrides } from '@ui';
import { isLocale } from '../../../ui/src/i18n/locales';
import type {
  CartItem,
  Customer,
  OrderInput,
  OrderReceipt,
  OrderStatus,
  Quote,
  QuoteInput,
} from '../api/checkout';
import { AuthError, call } from '../features/auth/api';
import { OrderSummary, OrderTotals, checkoutError } from '../features/store/OrderSummary';
import '../features/store/checkout.css';

const cartKey = 'modawana.store.cart';
const orderKey = 'modawana.store.last-order';
const attemptKey = 'modawana.store.pending-attempt';
const referencePattern = /^[0-9a-f]{32}$/;

function readCart(): CartItem[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(cartKey) ?? '[]');
    if (Array.isArray(value)) {
      return value
        .filter(
          (row): row is CartItem =>
            row &&
            typeof row.sku === 'string' &&
            /^[A-Z0-9][A-Z0-9._-]{0,47}$/.test(row.sku) &&
            Number.isInteger(row.quantity) &&
            row.quantity >= 1 &&
            row.quantity <= 99,
        )
        .slice(0, 20);
    }
  } catch {
    return [];
  }
  return [];
}

function saveCart(items: CartItem[]) {
  localStorage.setItem(cartKey, JSON.stringify(items));
}

function saved(key: string) {
  try {
    return localStorage.getItem(key) ?? '';
  } catch {
    return '';
  }
}

function remember(key: string, value?: string) {
  try {
    if (value) {
      localStorage.setItem(key, value);
    } else {
      localStorage.removeItem(key);
    }
  } catch {
    // The current page can still display the confirmation when storage is blocked.
  }
}

const definitiveFailures = new Set([
  'store_quote_changed',
  'store_stock',
  'store_coupon',
  'store_shipping',
  'invalid_fields',
  'not_found',
]);

for (const button of document.querySelectorAll<HTMLButtonElement>('[data-store-add]')) {
  const sku = button.dataset.storeAdd;
  if (sku) {
    button.hidden = false;
    button.addEventListener('click', () => {
      const items = readCart();
      const line = items.find((item) => item.sku === sku);
      if (!line && items.length === 20) {
        button.textContent = button.dataset.cartFull ?? button.textContent;
        return;
      }
      if (line) {
        line.quantity = Math.min(line.quantity + 1, 99);
      } else {
        items.push({ sku, quantity: 1 });
      }
      try {
        saveCart(items);
        button.textContent = button.dataset.added ?? button.textContent;
      } catch {
        button.textContent = button.dataset.cartFailed ?? button.textContent;
      }
    });
  }
}

function labelOverrides(): LabelOverrides | undefined {
  try {
    const json = document.getElementById('reader-labels')?.textContent;
    return json ? JSON.parse(json) : undefined;
  } catch {
    return undefined;
  }
}

const mount = document.querySelector<HTMLElement>('[data-store-checkout]');
const routeLocale = document.documentElement.lang;
if (mount && isLocale(routeLocale)) {
  createRoot(mount).render(
    <I18nProvider locale={routeLocale} overrides={labelOverrides()} syncDocument={false}>
      <Checkout />
    </I18nProvider>,
  );
}

const addressFields = [
  ['name', 'name', 120, true],
  ['email', 'email', 254, true],
  ['address', 'shipping address-line1', 180, true],
  ['address_2', 'shipping address-line2', 180, false],
  ['city', 'shipping address-level2', 100, true],
  ['region', 'shipping address-level1', 100, false],
  ['postal_code', 'shipping postal-code', 24, false],
] as const;

function Checkout() {
  const { locale, t } = useI18n();
  const root = '/public/store/checkout/' + locale;
  const [items, setItems] = useState(readCart);
  const [countries, setCountries] = useState<string[]>();
  const [country, setCountry] = useState('');
  const [coupon, setCoupon] = useState('');
  const [quote, setQuote] = useState<Quote>();
  const [order, setOrder] = useState<OrderStatus>();
  const [receipt, setReceipt] = useState<OrderReceipt>();
  const [notice, setNotice] = useState('');
  const [optionsNotice, setOptionsNotice] = useState('');
  const [paymentNotice, setPaymentNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<OrderInput>();
  const lock = useRef(false);
  const names = new Intl.DisplayNames(locale, { type: 'region' });

  useEffect(() => {
    let active = true;
    void call<{ countries: string[] }>(root + '/options')
      .then(({ countries }) => {
        if (active) {
          setCountries(countries);
        }
      })
      .catch((error: unknown) => {
        if (active) {
          setOptionsNotice(
            error instanceof AuthError && error.code === 'not_found'
              ? 'checkout.closed'
              : checkoutError(error),
          );
        }
      });
    const previous = saved(attemptKey);
    if (referencePattern.test(previous)) {
      const recover = async () => {
        try {
          const result = await call<OrderReceipt>(root + '/attempts/' + previous);
          if (active) {
            setReceipt(result);
            setItems([]);
            remember(attemptKey);
            remember(orderKey, result.reference);
            remember(cartKey);
            const current = await call<OrderStatus>(root + '/orders/' + result.reference);
            if (active) {
              setOrder(current);
            }
          }
        } catch (error) {
          if (active && error instanceof AuthError && error.code === 'not_found') {
            remember(attemptKey);
          } else if (active) {
            setNotice(checkoutError(error));
          }
        }
      };
      void recover();
    }
    const params = new URLSearchParams(window.location.search);
    const reference = params.get('order') ?? '';
    const result = params.get('paypal');
    if (referencePattern.test(reference) && (result === 'return' || result === 'cancel')) {
      window.history.replaceState(null, '', window.location.pathname);
      setBusy(true);
      lock.current = true;
      const path = root + '/orders/' + reference;
      const response =
        result === 'return'
          ? call<OrderStatus>(path + '/paypal/finish', 'POST', {})
          : call<OrderStatus>(path);
      if (result === 'cancel') {
        setPaymentNotice('paypal.cancelled');
      }
      void response
        .then((status) => {
          if (active) {
            setOrder(status);
            setReceipt(status);
            remember(orderKey, status.reference);
          }
        })
        .catch(async (error: unknown) => {
          if (active) {
            setNotice(checkoutError(error));
          }
          try {
            const status = await call<OrderStatus>(path);
            if (active) {
              setOrder(status);
            }
          } catch {
            // Authorized lookup may be unavailable if this is another browser.
          }
        })
        .finally(() => {
          if (active) {
            lock.current = false;
            setBusy(false);
          }
        });
    }
    return () => {
      active = false;
    };
  }, [root]);

  function changeCart(next: CartItem[]) {
    setQuote(undefined);
    setPending(undefined);
    setItems(next);
    try {
      saveCart(next);
    } catch {
      setNotice('checkout.cartFailed');
    }
  }

  async function perform(action: () => Promise<void>) {
    if (lock.current) {
      return;
    }
    lock.current = true;
    setBusy(true);
    setNotice('');
    try {
      await action();
    } catch (error) {
      setNotice(checkoutError(error));
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }

  function input(): QuoteInput {
    return { locale, country, coupon, items };
  }

  function makeQuote(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setQuote(undefined);
    setPending(undefined);
    void perform(async () => setQuote(await call<Quote>(root + '/quote', 'POST', input())));
  }

  async function submitOrder(request: OrderInput) {
    let result: OrderReceipt;
    try {
      result = await call<OrderReceipt>(root + '/orders', 'POST', request);
    } catch (error) {
      if (error instanceof AuthError && definitiveFailures.has(error.code)) {
        remember(attemptKey);
        setPending(undefined);
        if (error.code !== 'invalid_fields') {
          setQuote(undefined);
        }
      }
      throw error;
    }
    setPending(undefined);
    setQuote(undefined);
    setReceipt(result);
    setItems([]);
    remember(attemptKey);
    remember(orderKey, result.reference);
    remember(cartKey);
    setOrder(await call<OrderStatus>(root + '/orders/' + result.reference));
  }

  function placeOrder(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!quote || lock.current) {
      return;
    }
    const fields = new FormData(event.currentTarget);
    const customer = Object.fromEntries(
      addressFields.map(([key]) => [key, String(fields.get(key) ?? '').trim()]),
    ) as unknown as Customer;
    const request = {
      ...input(),
      customer,
      key: crypto.randomUUID().replaceAll('-', ''),
      quote_fingerprint: quote.fingerprint,
    };
    setPending(request);
    remember(attemptKey, request.key);
    void perform(() => submitOrder(request));
  }

  function lookup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const reference = String(new FormData(event.currentTarget).get('reference') ?? '')
      .trim()
      .toLowerCase();
    if (referencePattern.test(reference)) {
      setOrder(undefined);
      void perform(async () => setOrder(await call<OrderStatus>(root + '/orders/' + reference)));
    }
  }

  function paymentAction(action: 'approval' | 'check' | 'finish') {
    if (!order) {
      return;
    }
    const path = root + '/orders/' + order.reference + '/paypal/' + action;
    setPaymentNotice('');
    void perform(async () => {
      if (action === 'approval') {
        const { approval_url } = await call<{ approval_url: string }>(path, 'POST', {});
        window.location.assign(approval_url);
      } else {
        const status = await call<OrderStatus>(path, 'POST', {});
        setOrder(status);
      }
    });
  }

  return (
    <div className="store-checkout">
      {notice && (
        <p className="public-notice" role="alert">
          {t(notice)}
        </p>
      )}
      {optionsNotice && <p className="public-notice">{t(optionsNotice)}</p>}
      {paymentNotice && <p role="status">{t(paymentNotice)}</p>}
      {!countries && !optionsNotice && <p role="status">{t('public.loading')}</p>}
      {receipt && (
        <p role="status">
          {t('checkout.reserved')} <bdi>{receipt.reference}</bdi>
        </p>
      )}
      {countries && items.length === 0 && !receipt && <p>{t('checkout.empty')}</p>}
      {countries && items.length > 0 && (
        <>
          <form onSubmit={makeQuote} className="vb-card store-checkout-form">
            <fieldset disabled={busy || !!pending}>
              <legend>{t('checkout.cart')}</legend>
              <ul className="store-cart-items">
                {items.map((item) => (
                  <li key={item.sku}>
                    <Field label={item.sku}>
                      <input
                        type="number"
                        required
                        min={1}
                        max={99}
                        value={item.quantity}
                        onChange={(event) => {
                          const quantity = Number(event.target.value);
                          if (Number.isInteger(quantity) && quantity >= 1 && quantity <= 99) {
                            changeCart(
                              items.map((row) =>
                                row.sku === item.sku ? { ...row, quantity } : row,
                              ),
                            );
                          }
                        }}
                      />
                    </Field>
                    <Button
                      type="button"
                      onClick={() => changeCart(items.filter((row) => row.sku !== item.sku))}
                    >
                      {t('checkout.remove')}
                    </Button>
                  </li>
                ))}
              </ul>
              <Field label={t('checkout.country')}>
                <select
                  required
                  value={country}
                  autoComplete="shipping country"
                  onChange={(event) => {
                    setCountry(event.target.value);
                    setQuote(undefined);
                  }}
                >
                  <option value="">{t('checkout.chooseCountry')}</option>
                  {countries.map((code) => (
                    <option key={code} value={code}>
                      {names.of(code) ?? code}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label={t('checkout.coupon')}>
                <input
                  value={coupon}
                  maxLength={32}
                  dir="ltr"
                  onChange={(event) => {
                    setCoupon(event.target.value);
                    setQuote(undefined);
                  }}
                />
              </Field>
              <Button type="submit">{t('checkout.calculate')}</Button>
            </fieldset>
          </form>
          {quote && (
            <form onSubmit={placeOrder} className="vb-card store-checkout-form">
              <fieldset disabled={busy || !!pending}>
                <legend>{t('checkout.review')}</legend>
                <OrderTotals data={quote} />
                {addressFields.map(([key, autocomplete, maxLength, required]) => (
                  <Field label={t('checkout.customer.' + key)} key={key}>
                    <input
                      name={key}
                      autoComplete={autocomplete}
                      type={key === 'email' ? 'email' : 'text'}
                      maxLength={maxLength}
                      required={required}
                      dir={key === 'email' ? 'ltr' : 'auto'}
                    />
                  </Field>
                ))}
                <p>{t('checkout.reservationOnly')}</p>
                <Button type="submit">{t('checkout.place')}</Button>
              </fieldset>
              {pending && (
                <div className="store-checkout-actions">
                  <Button disabled={busy} onClick={() => void perform(() => submitOrder(pending))}>
                    {t('checkout.retrySame')}
                  </Button>
                </div>
              )}
            </form>
          )}
        </>
      )}
      <form onSubmit={lookup} className="vb-card store-checkout-form">
        <fieldset disabled={busy}>
          <legend>{t('checkout.lookup')}</legend>
          <Field label={t('checkout.reference')}>
            <input
              name="reference"
              key={receipt?.reference ?? 'reference'}
              defaultValue={receipt?.reference ?? saved(orderKey)}
              required
              dir="ltr"
              minLength={32}
              maxLength={32}
              pattern="[0-9a-fA-F]{32}"
              autoComplete="off"
            />
          </Field>
          <Button type="submit">{t('checkout.find')}</Button>
        </fieldset>
      </form>
      {order && (
        <div className="vb-card">
          <OrderSummary order={order} />
          <div className="store-checkout-actions">
            {order.status === 'awaiting_payment' && countries && (
              <Button variant="primary" disabled={busy} onClick={() => paymentAction('approval')}>
                {t('paypal.pay')}
              </Button>
            )}
            {(order.status === 'awaiting_payment' ||
              order.payment === 'uncertain' ||
              order.status === 'expired') && (
              <Button disabled={busy} onClick={() => paymentAction('check')}>
                {t('paypal.check')}
              </Button>
            )}
            {order.payment === 'uncertain' && countries && (
              <Button disabled={busy} onClick={() => paymentAction('finish')}>
                {t('paypal.retryPayment')}
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
