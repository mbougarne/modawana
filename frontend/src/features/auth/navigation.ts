const adminPath = /^\/(en|ar|fr)\/admin(?:\/|$)/;
const returnOrigin = 'https://owner-return.invalid';

export function isOwnerRequest(path: string): boolean {
  return (
    typeof window !== 'undefined' &&
    adminPath.test(window.location.pathname) &&
    !/^\/(public|learning|auth)(\/|$)/.test(path)
  );
}

export function ownerReturnTarget(value: string | null): string | null {
  if (
    !value ||
    value.includes('\\') ||
    [...value].some((character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)
  ) {
    return null;
  }
  try {
    const target = new URL(value, returnOrigin);
    if (
      !value.startsWith('/') ||
      target.origin !== returnOrigin ||
      !adminPath.test(target.pathname) ||
      /%2f|%5c/i.test(target.pathname) ||
      /\/admin\/login\/?$/.test(target.pathname)
    ) {
      return null;
    }
    return target.pathname + target.search + target.hash;
  } catch {
    return null;
  }
}

export function redirectOwnerToLogin(
  location: Pick<Location, 'pathname' | 'search' | 'replace'> | undefined = typeof window ===
  'undefined'
    ? undefined
    : window.location,
): void {
  const match = location?.pathname.match(adminPath);
  if (!location || !match) {
    return;
  }
  const target = ownerReturnTarget(location.pathname + location.search);
  location.replace(`/${match[1]}/login${target ? '?return=' + encodeURIComponent(target) : ''}`);
}
