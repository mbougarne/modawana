import type { Brand } from '@ui';
import type { SiteSettings } from '../../../api/configuration';

export const iconNames = [
  'menu',
  'code',
  'link',
  'search',
  'moon',
  'github',
  'linkedin',
  'rss',
  'lock',
  'image',
  'video',
];

export function siteBrand(s: SiteSettings): Brand {
  return {
    monogram: s.monogram,
    primary: s.primary,
    secondary: s.secondary,
    logo:
      s.logo_mode === 'image' && s.logo_media_id
        ? { kind: 'image', url: '/media/' + s.logo_media_id, alt: s.wordmark }
        : s.logo_mode === 'monogram'
          ? { kind: 'monogram', letters: s.monogram }
          : { kind: 'wordmark', text: s.wordmark },
  };
}
