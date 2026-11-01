import { useEffect, useRef, useState } from 'react';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';

export function OwnerLanguageMenu({ path }: { path: string }) {
  const { locale, t } = useI18n();
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) {
      return;
    }
    menu.current?.querySelector('a')?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false);
        trigger.current?.focus();
      }
    };
    const outside = (e: PointerEvent) => {
      if (
        !menu.current?.contains(e.target as Node) &&
        !trigger.current?.contains(e.target as Node)
      ) {
        setOpen(false);
      }
    };
    document.addEventListener('keydown', key);
    document.addEventListener('pointerdown', outside);
    return () => {
      document.removeEventListener('keydown', key);
      document.removeEventListener('pointerdown', outside);
    };
  }, [open]);
  return (
    <div className="config-language">
      <button
        type="button"
        className="vb-icon"
        ref={trigger}
        aria-label={t('foundation.language')}
        aria-expanded={open}
        aria-controls="config-language-menu"
        onClick={() => setOpen((v) => !v)}
      >
        {locale === 'ar' ? 'ع' : locale.toUpperCase()}
      </button>
      {open && (
        <div id="config-language-menu" ref={menu}>
          {(['en', 'fr', 'ar'] as const).map((l) => (
            <a
              key={l}
              href={`/${l}${path}`}
              lang={l}
              dir={l === 'ar' ? 'rtl' : 'ltr'}
              aria-current={l === locale ? 'page' : undefined}
            >
              {t('language.' + l)}
            </a>
          ))}
        </div>
      )}
    </div>
  );
}
