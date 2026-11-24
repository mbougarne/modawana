import type { Locale } from './content';

export interface StoreSettings {
  version: number;
  enabled: boolean;
  currency: string;
  digits: number;
}

export interface StoreCategory {
  key: string;
  locale: Locale;
  title: string;
  version: number;
}

export interface StoreImage {
  id: string;
  alt: string;
}

export interface StoreDraftInput {
  version: number;
  slug: string;
  title: string;
  description: string;
  category: string;
  images: StoreImage[];
}

export interface StoreVariantInput {
  version: number;
  labels: Partial<Record<Locale, string>>;
  price_minor: number;
  active: boolean;
}

export interface StoreVariant extends StoreVariantInput {
  sku: string;
  stock: number;
  reserved: number;
}

export interface StoreDraft extends StoreDraftInput {
  key: string;
  locale: Locale;
  published: boolean;
  published_version: number;
  public_url?: string;
  variants: StoreVariant[];
}

export interface StoreWorkspace {
  settings: StoreSettings;
  categories: StoreCategory[];
  products: { key: string; title: string; localized: boolean; published: boolean }[];
  next?: string;
}

export interface StockAdjustment {
  id: string;
  sku: string;
  actor: string;
  before: number;
  after: number;
  delta: number;
  reason: string;
  created_at: string;
}

export interface PublicStoreProduct {
  key: string;
  locale: Locale;
  slug: string;
  url: string;
  title: string;
  description: string;
  category?: string;
  published_at: string;
  price_minor: number;
  images: StoreImage[];
  variants?: { sku: string; label: string; price_minor: number; in_stock: boolean }[];
}

export interface PublicStore {
  checkout_enabled: boolean;
  currency: string;
  digits: number;
  product?: PublicStoreProduct;
  products?: PublicStoreProduct[];
  categories?: Pick<StoreCategory, 'key' | 'title'>[];
  total: number;
  next_cursor?: string;
}
