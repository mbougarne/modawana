/** Unicode letters/numbers match the Go slug contract; accents are folded for readable URLs. */
export function slugFromTitle(title: string, max = 160): string {
  return Array.from(
    title
      .normalize('NFKD')
      .replace(/\p{M}/gu, '')
      .toLowerCase()
      .replace(/[^\p{L}\p{N}]+/gu, '-')
      .replace(/^-+|-+$/g, ''),
  )
    .slice(0, max)
    .join('')
    .replace(/-+$/, '');
}

export function titleSlugChange(
  title: string,
  slug: string,
  nextTitle: string,
  manual: boolean,
  locked = false,
  max = 160,
): string {
  return locked || manual || (slug && slug !== slugFromTitle(title, max))
    ? slug
    : slugFromTitle(nextTitle, max);
}
