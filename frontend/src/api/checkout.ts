import type { Locale } from './content';

export interface CartItem {
  sku: string;
  quantity: number;
}

export interface QuoteInput {
  locale: Locale;
  country: string;
  coupon: string;
  items: CartItem[];
}

export interface Customer {
  name: string;
  email: string;
  address: string;
  address_2: string;
  city: string;
  region: string;
  postal_code: string;
}

export interface OrderLine extends CartItem {
  title: string;
  variant: string;
  unit_minor: number;
  line_minor: number;
}

export interface Snapshot {
  currency: string;
  digits: number;
  country: string;
  lines: OrderLine[];
  shipping_zone: string;
  coupon: string;
  tax_bps: number;
  tax_shipping: boolean;
  subtotal_minor: number;
  discount_minor: number;
  shipping_minor: number;
  tax_minor: number;
  total_minor: number;
}

export interface Quote extends Snapshot {
  fingerprint: string;
}

export interface OrderInput extends QuoteInput {
  customer: Customer;
  key: string;
  quote_fingerprint: string;
}

export interface OrderReceipt {
  reference: string;
  created_at: number;
  expires_at: number;
}

export interface OrderStatus extends OrderReceipt {
  snapshot: Snapshot;
  status: string;
  payment: string;
  refund: string;
  fulfillment: string;
}

export interface MerchantOrder extends OrderStatus {
  customer: Customer;
  locale: Locale;
  operation: string;
  reservation: string;
  events: { kind: string; created_at: number }[];
}

export interface Orders {
  orders: (OrderReceipt & { status: string })[];
  next?: string;
}

export interface ShippingZone {
  name: string;
  countries: string[];
  rate_minor: number;
  tax_bps: number;
  tax_shipping: boolean;
}

export interface Coupon {
  code: string;
  discount_minor: number;
  discount_bps: number;
}

export interface CheckoutSettings {
  version: number;
  enabled: boolean;
  zones: ShippingZone[];
  coupons: Coupon[];
}
