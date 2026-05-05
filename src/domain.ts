export const baseDomain = "web.oboard.fun";

export function getSessionSlug(hostname: string, domain = baseDomain) {
  const host = hostname.toLowerCase();
  if (host === domain || host === "localhost" || host === "127.0.0.1") {
    return "";
  }
  if (!host.endsWith(`.${domain}`)) {
    return "";
  }
  return host.slice(0, -(domain.length + 1)).split(".")[0] ?? "";
}

export function isValidSlug(slug: string) {
  return /^[a-z0-9](?:[a-z0-9-]{6,61}[a-z0-9])$/.test(slug);
}

export function buildSessionURL(slug: string, protocol: string, domain = baseDomain) {
  const scheme = protocol === "https:" ? "https" : "http";
  return `${scheme}://${slug}.${domain}/`;
}
