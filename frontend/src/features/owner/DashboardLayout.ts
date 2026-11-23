import { createContext, useContext } from 'react';
import type { Localized } from '../../api/configuration';

export type DashboardItem = { id: string; rename?: Localized; group?: string; hidden?: boolean };

export type DashboardAppearance = {
  theme?: 'classic' | 'slate';
  density?: 'comfortable' | 'compact';
};

export type DashboardPreset = { name: string; appearance: DashboardAppearance };

export type DashboardLayout = {
  navigation?: DashboardItem[];
  widgets?: DashboardItem[];
  appearance: DashboardAppearance;
  home?: string;
  presets?: DashboardPreset[];
};

export type DashboardChoice = { id: string; label?: Localized };

export type DashboardState = {
  site: { version: number; layout: DashboardLayout | null };
  personal: { version: number; layout: DashboardLayout | null };
  registry: { navigation: DashboardChoice[]; widgets: DashboardChoice[] };
};

export const dashboardURL = '/api/v1/dashboard-layout';

export const dashboardGroups = ['content', 'site', 'operations'] as const;

export const DashboardLayoutContext = createContext<{
  state?: DashboardState;
  error?: number;
  reload: () => void;
}>({ reload: () => {} });

export function useDashboardLayout() {
  return useContext(DashboardLayoutContext);
}

export function appliedLayout(state?: DashboardState) {
  return state?.personal.layout ?? state?.site.layout;
}

export function orderedItems<T extends { id: string }>(
  registered: T[],
  items: DashboardItem[] = [],
) {
  const byID = new Map(registered.map((item) => [item.id, item]));
  const result: { target: T; item: DashboardItem }[] = [];
  for (const item of items) {
    const target = byID.get(item.id);
    if (target) {
      byID.delete(item.id);
      result.push({ target, item });
    }
  }
  for (const target of byID.values()) {
    result.push({ target, item: { id: target.id } });
  }
  return result;
}
