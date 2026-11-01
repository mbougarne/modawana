import type { PublicConfiguration } from '../../api/configuration';
import type { PageRoute } from '../../api/content';

// Keep SPA navigation consistent with the Go HTML shell. Only the public media
// manifest can supply a cover; learner-only lesson covers are never social images.
export function socialMetadata(
  page: PageRoute,
  configuration: PublicConfiguration | undefined,
  origin: string,
  title: string,
) {
  let image = origin + '/social-card.png';
  let alt = title;
  let width = 1200;
  let height = 630;
  let type = 'website';
  const site = configuration?.site;
  const profile = configuration?.profile;
  const siteName = configuration?.effective.name?.text || site?.wordmark || '';
  if (page.content && page.kind !== 'lesson' && !page.content.locked) {
    type = 'article';
    const cover = page.content.media?.find(
      (m) =>
        m.id === page.content?.cover_media_id &&
        m.kind === 'image' &&
        ['image/jpeg', 'image/png', 'image/webp'].includes(m.mime_type),
    );
    if (cover) {
      return {
        image: origin + '/media/' + cover.id,
        alt,
        width: cover.width,
        height: cover.height,
        type,
        siteName,
      };
    }
  }
  let id;
  if (profile?.banner_enabled && profile.banner_media_id) {
    id = profile.banner_media_id;
    alt = configuration?.effective.banner_alt?.text || title;
  } else if (profile?.portrait_media_id) {
    id = profile.portrait_media_id;
    alt = configuration?.effective.portrait_alt?.text || title;
  } else if (site?.logo_mode === 'image' && site.logo_media_id) {
    id = site.logo_media_id;
    alt = siteName || title;
  }
  if (id) {
    image = origin + '/media/' + id;
    width = 0;
    height = 0;
  }
  return { image, alt, width, height, type, siteName };
}

export function updateSocialMetadata(
  social: ReturnType<typeof socialMetadata> | undefined,
  title: string,
  description: string,
) {
  document.head
    .querySelectorAll(
      'meta[property^="og:image"],meta[property="og:type"],meta[property="og:site_name"],meta[name^="twitter:"]',
    )
    .forEach((n) => n.remove());
  if (!social) {
    return;
  }
  const properties: Record<string, string> = {
    'og:type': social.type,
    'og:site_name': social.siteName,
    'og:image': social.image,
    'og:image:alt': social.alt,
  };
  if (social.width && social.height) {
    properties['og:image:width'] = String(social.width);
    properties['og:image:height'] = String(social.height);
  }
  for (const [property, content] of Object.entries(properties)) {
    const meta = document.createElement('meta');
    meta.setAttribute('property', property);
    meta.content = content;
    document.head.append(meta);
  }
  for (const [name, content] of Object.entries({
    'twitter:card': 'summary_large_image',
    'twitter:title': title,
    'twitter:description': description,
    'twitter:image': social.image,
    'twitter:image:alt': social.alt,
  })) {
    const meta = document.createElement('meta');
    meta.name = name;
    meta.content = content;
    document.head.append(meta);
  }
}
