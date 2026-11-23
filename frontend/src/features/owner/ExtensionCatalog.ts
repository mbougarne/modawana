import { createContext, useContext } from 'react';
import type { Locale } from '../../api/content';

type Localized = Partial<Record<Locale, string>> & { en: string };

export type Contribution = {
  kind:
    | 'builder_block'
    | 'admin_navigation'
    | 'settings'
    | 'widget'
    | 'public_projection'
    | 'provider_adapter';
  key: string;
  version: number;
  label: Localized;
  target: string;
  text?: Localized;
};

export type RegisteredContribution = Contribution & { extension: string };

export const ExtensionCatalog = createContext<{
  items: RegisteredContribution[];
  refresh: () => void;
}>({
  items: [],
  refresh: () => {},
});

export function useExtensionCatalog() {
  return useContext(ExtensionCatalog);
}

export function localizedContribution(text: Localized, locale: Locale) {
  return text[locale] || text.en;
}

export function contributionKey(c: RegisteredContribution) {
  return c.extension + '.' + c.key;
}
