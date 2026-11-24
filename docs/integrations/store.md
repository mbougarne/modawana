# Store catalog and inventory

[العربية](store.ar.md) · [Documentation](../README.md)

Store is an optional first-party module for one merchant selling physical
goods per installation. The catalog is disabled by default. The owner can
enable it in **Store** and select a three-letter currency before adding
the first SKU. Prices are integer minor units. For example, enter `12345`
for MAD 123.45, or `12345` for JPY 12345.

## Catalog and storefront

Use the catalog language selector to edit localizations. A stable product key
connects translations. Each localization has its own slug, title, description,
optional category, and up to eight uploaded images with localized alt text.
Use the same stable category key to connect category labels across languages.

Create the first active variant before publishing. Each SKU is unique across
the whole store, is normalized to uppercase, and has its own price and stock.
Variant labels may be translated. The SKU is shown when a localized label is
missing. Price, active status, and stock are shared across localizations and
take effect when saved. The last active variant of a published product cannot
be deactivated.

Save a localized draft, then publish it explicitly. Subsequent edits to the
localized text, URL, category assignment, or gallery remain in the draft
until publication. Go serves the catalog at `/{locale}/store` and products at
`/{locale}/store/{slug}`. The store, search, canonical metadata, and product image
access use the same publication and locale visibility rules. Store browsing
supports search and category filters. Public pages show availability while
exact stock quantities and adjustment history stay in the workspace.

## Inventory and access

Variants start with zero stock. Use **Record adjustment** to enter a signed
quantity and a reason. The same transaction changes stock and records the actor,
previous quantity, new quantity, and timestamp. Stock stays between 0 and
1,000,000,000. An exact retry returns the original receipt. Changing the inputs
for the same operation ID returns a conflict. The workspace shows the latest
50 adjustments for a SKU.

The installation Owner controls storefront activation and currency. The Owner
and delegated Store Managers can edit localizations and categories, manage store
images and variants, publish, and adjust inventory. Store Managers enter through
their usual staff workspace. Store permissions are rechecked in write
transactions. Store access does not grant access to the blog editor or its
private uploads.

## Guest checkout and orders

Under **Store → Checkout settings**, the installation Owner can enable guest
reservations separately from the storefront. Add at least one shipping zone
with unique ISO two-letter country codes. Each zone has a flat shipping rate,
a tax rate in basis points (100 is 1%), and a choice of whether to tax
shipping. Coupons apply one fixed minor-unit amount or one percentage to the
merchandise subtotal, with a maximum discount of that subtotal. The server
applies the discount before tax and rounds tax once to the nearest minor
unit. All amounts use the Store currency.

A guest adds published variants from a product page, edits quantities at
`/{locale}/store/checkout/cart`, and reviews the server-calculated totals
before submitting contact and shipping details. Changed prices, availability,
or totals require a fresh quote. The browser stores cart SKUs and quantities
locally. A private browser cookie scopes order lookup and retry recovery. The
order reference can be used on the same browser while the cookie is available.
Guest responses show the frozen order lines, totals and statuses. Contact and
address snapshots are available in **Store → Orders** for the Owner and
delegated Store Managers.

Order creation records the exact line and total snapshots, a pending payment
operation, and stock reservations in one transaction. Exact retries return
the original receipt. Held stock is unavailable to other guests and protected
from merchant stock decrements. Unpaid reservations expire after 30 minutes,
and a scheduled job releases them. A capture awaiting verification retains its
hold until the provider result is verified. Payment confirmed after a release
appears in the merchant's review queue through its order status and history.
It leaves inventory and other orders intact. Payment, refund and fulfillment
have separate statuses.

Disabling checkout stops new reservations and captures. Prior receipts, order
lookup and reconciliation remain available. See the
[OpenAPI schema](../api/v1.openapi.json) for Store endpoints.

## PayPal setup

Each store uses a PayPal REST app owned by its merchant. Give the server
`BLOG_PAYPAL_CONNECTIONS` as JSON through the service environment. Keep the
secret in a private regular file (0600) outside the checkout and data root.
The example contains placeholders.

```json
{
  "active": "store-sandbox",
  "connections": [
    {
      "id": "store-sandbox",
      "mode": "sandbox",
      "merchant_id": "PAYPAL_MERCHANT_ID",
      "client_id": "PAYPAL_REST_APP_CLIENT_ID",
      "client_secret_file": "/run/secrets/paypal-client-secret",
      "webhook_id": "PAYPAL_WEBHOOK_ID"
    }
  ]
}
```

In the same PayPal app, register your public HTTPS endpoint
`/api/v1/webhooks/paypal/store-sandbox`. Subscribe to order approval and capture
events, including `CHECKOUT.ORDER.APPROVED`, `PAYMENT.CAPTURE.PENDING`,
`PAYMENT.CAPTURE.COMPLETED`, `PAYMENT.CAPTURE.DENIED`,
`PAYMENT.CAPTURE.REFUNDED`, `PAYMENT.CAPTURE.REVERSED`,
`PAYMENT.REFUND.PENDING`, and `PAYMENT.REFUND.FAILED` where offered by the app.
Set the resulting webhook ID and restart Modawana. Connection IDs are anchored
to one environment, merchant and REST app. Change the connection ID when switching
any of those. Set `active` to an empty string and restart to disconnect new checkout.
Keep former credentials available in the list while reconciling their orders
and refunds. Rotating a secret for the same REST app keeps the operation binding.

Select a [PayPal-supported currency](https://developer.paypal.com/api/codes/currency/)
before creating variants. Use a currency the merchant can receive. Enable the
storefront and PayPal checkout under **Store → Checkout settings**. Checkout
requires a positive payable total. PayPal uses whole units for HUF, JPY and TWD.

## Payment, refunds and recovery

From their reservation, the buyer continues to PayPal and approves on PayPal's
site. Modawana sends the shipping name and address and the frozen amount to
PayPal. PayPal handles payment credentials. Modawana checks the provider order,
merchant, currency, amount, capture status and identifiers before settlement.
A return to the cart only starts this check. Verified webhook notifications are
deduplicated and reconciled against current PayPal data. Unmatched operations and
reversals appear under **PayPal notifications** for review.

In **Store → Orders**, the installation Owner can check the provider status and
confirm a refund in minor units after fresh authentication. Enter the full
remaining amount for a full refund or a smaller amount for a partial refund.
Refunds have independent statuses and do not restock inventory. For an
interrupted refund, use **Check or resume refund** on its existing entry. The
system reuses the stored operation ID within its 30-minute retry window. Older
uncertain attempts require checking PayPal and the receipt history before taking
further action.

A restored installation closes checkout and pauses refund writes while preserving
orders, operation IDs and receipt history. After the owner has recovered access,
compare PayPal activity since the backup against the restored ledger. Resolve
unmatched captures and refunds in PayPal. Confirm reconciliation under
**Store → Checkout settings → PayPal**, then enable checkout when ready.

## Sandbox acceptance

Use a dedicated sandbox merchant and buyer, a public HTTPS callback, a distinct
sandbox connection ID and a supported currency such as USD. Place an order and
verify approval, capture, the stock change, and the merchant record. Verify that
cancelling in PayPal preserves the unpaid reservation until it expires and that
two browsers competing for the final unit produce one reservation. Resend a
signed notification and resend an older notification after settlement. Verify
that neither causes a duplicate stock change. Simulate a dropped capture
response and use the status check to recover. Test one full refund and a partial
refund followed by refunding the remainder on another order. Record the actual
sandbox results before configuring a live merchant.

[PayPal Orders v2](https://developer.paypal.com/api/orders/v2/) ·
[Webhook verification](https://developer.paypal.com/api/webhooks/v1/) ·
[Payments and refunds](https://developer.paypal.com/api/payments/v2/captures-refund) ·
[Request idempotency](https://developer.paypal.com/api/rest/requests/)

## Merchant orders and fulfillment

The installation Owner and delegated Store Managers can open **Store → Orders**,
read the frozen customer and address snapshots, and inspect the payment
references, refunds, shipment and activity history. Store Managers use their
staff workspace. Refunds remain an installation Owner action and require fresh
authentication. The history records refund requests, retries and verified
outcomes against their original operation IDs and amounts.

Cancel an unfulfilled order with a reason. An unpaid cancellation releases its
hold. A payment awaiting provider verification keeps its hold and cannot be
cancelled. Cancelling a paid order changes its fulfillment state. Use the separate
refund and stock actions for money and returned inventory. A payment verified
after an unpaid cancellation remains in review.

Mark a confirmed, paid order as shipped and optionally supply a carrier and
tracking number. Both values can be corrected together later. Buyers can see
the shipment and tracking details in their original guest browser. Payment
confirmation and shipment notices are queued in the same transaction as their
order changes. The outbox delivers them to the order's private in-app
notifications and retries interrupted delivery. The notices appear in the
guest lookup and merchant order detail.

Use **Return units to stock** for a sold item that has actually been returned.
The action is available for shipped and cancelled orders, or sold orders that
have been fully refunded. Choose a SKU, a quantity and a reason. The order
history and inventory adjustment both record the action. Cumulative returns
cannot exceed that order's sold quantity.

Use **Payment receipt** on a verified payment to open a printable document with
line items, the original paid total and the total amount refunded. The guest
copy requires the originating browser cookie. **Export orders (CSV)** is
available to the Owner and Store Managers. It includes one row per order with
customer and shipping snapshots, statuses, payment and refund references,
minor-unit totals, and line and restock details. Handle the file as private
customer data.
