import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { brandCss } from './brand';

export type ThemeChoice = 'system' | 'light' | 'dark';
interface ThemeCtx {
  choice: ThemeChoice;
  resolved: 'light' | 'dark';
  setChoice: (c: ThemeChoice) => void;
  toggle: () => void;
}
const Ctx = createContext<ThemeCtx | null>(null);
const KEY = 'theme';

const read = (): ThemeChoice => {
  try {
    const v = localStorage.getItem(KEY);
    return v === 'light' || v === 'dark' ? v : 'system';
  } catch {
    return 'system';
  }
};
const systemDark = () =>
  typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches;

/**
 * Light / dark / system. "system" sets no data-theme, so tokens.css follows prefers-color-scheme.
 * An explicit choice sets <html data-theme> and is remembered in localStorage.
 * To avoid a flash, also inline this in <head> before the CSS:
 *   <script>try{var t=localStorage.getItem('theme');if(t)document.documentElement.dataset.theme=t}catch(e){}</script>
 */
export function ThemeProvider({
  children,
  initial,
}: {
  children: ReactNode;
  initial?: ThemeChoice;
}) {
  const [choice, setChoiceState] = useState<ThemeChoice>(
    () => initial ?? (typeof window === 'undefined' ? 'system' : read()),
  );
  const [sysDark, setSysDark] = useState(() =>
    typeof window === 'undefined' ? false : systemDark(),
  );

  useEffect(() => {
    if (typeof matchMedia !== 'function') {
      return;
    }
    const mq = matchMedia('(prefers-color-scheme: dark)');
    const on = () => setSysDark(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);

  useEffect(() => {
    const el = document.documentElement;
    if (choice === 'system') {
      delete el.dataset.theme;
    } else {
      el.dataset.theme = choice;
    }
    try {
      if (choice === 'system') {
        localStorage.removeItem(KEY);
      } else {
        localStorage.setItem(KEY, choice);
      }
    } catch {
      /* storage unavailable */
    }
  }, [choice]);

  const resolved = choice === 'system' ? (sysDark ? 'dark' : 'light') : choice;
  const setChoice = useCallback((c: ThemeChoice) => setChoiceState(c), []);
  const toggle = useCallback(
    () => setChoiceState(resolved === 'dark' ? 'light' : 'dark'),
    [resolved],
  );
  const value = useMemo(
    () => ({ choice, resolved, setChoice, toggle }),
    [choice, resolved, setChoice, toggle],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTheme(): ThemeCtx {
  const v = useContext(Ctx);
  if (!v) {
    throw new Error('useTheme must be used inside <ThemeProvider>');
  }
  return v;
}

/** Applies the owner's saved primary/secondary colours site-wide. Renders nothing for the defaults. */
export function BrandStyle({
  primary,
  secondary,
  nonce,
}: {
  primary: string;
  secondary: string;
  nonce?: string;
}) {
  const css = brandCss(primary, secondary);
  return css ? (
    <style data-brand nonce={nonce}>
      {css}
    </style>
  ) : null;
}
