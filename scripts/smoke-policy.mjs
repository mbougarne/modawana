// Keep selection explicit: local runs default to release-level coverage.
export function smokeSuite(value = process.env.BLOG_SMOKE_SUITE || 'full') {
  if (!['quick', 'full'].includes(value)) {
    throw new Error(`Unknown BLOG_SMOKE_SUITE: ${value}`);
  }
  return value;
}

export function smokeLocales(suite = smokeSuite(), override = process.env.BLOG_TEST_LOCALES) {
  const locales = (override ?? (suite === 'quick' ? 'en' : 'en,ar,fr')).split(',');
  if (!locales.length || locales.some((locale) => !['en', 'ar', 'fr'].includes(locale))) {
    throw new Error('BLOG_TEST_LOCALES must contain en, ar or fr');
  }
  return [...new Set(locales)];
}

export function visualCases(suite = smokeSuite()) {
  if (suite === 'quick') {
    return [
      { locale: 'en', width: 1280, theme: 'light' },
      { locale: 'ar', width: 320, theme: 'dark' },
      { locale: 'fr', width: 390, theme: 'light' },
    ];
  }
  return ['en', 'ar', 'fr'].flatMap((locale) =>
    [320, 390, 1280].flatMap((width) =>
      ['light', 'dark'].map((theme) => ({ locale, width, theme })),
    ),
  );
}

export function acceptanceCases(suite = smokeSuite()) {
  return suite === 'quick'
    ? [{ width: 320, theme: 'light' }]
    : [320, 390, 1280].flatMap((width) => ['light', 'dark'].map((theme) => ({ width, theme })));
}
