import type { PageRoute } from '../../api/content';

export function updateSocialMetadata(
  social: NonNullable<PageRoute['metadata']>['social'] | undefined,
  title: string,
  description: string,
) {
  document.head
    .querySelectorAll(
      'meta[property^="og:image"],meta[property="og:type"],meta[property="og:site_name"],meta[name^="twitter:"]',
    )
    .forEach((n) => n.remove());
  if (!social?.image) {
    return;
  }
  const properties: Record<string, string> = {
    'og:type': social.type,
    'og:site_name': social.site_name,
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
