import type { ReactNode } from 'react';
import { useAssets, useFavicon } from './AssetContext';
import { hostnameOf, monogramHue, monogramLetter, registrableDomain } from './paths';

/** Site mark: a real favicon the engine already fetched, else a letter tile on one of six token hues. */
export function SiteIcon({ domain }: { domain: string }) {
  const favicon = useFavicon(domain);
  if (favicon) return <img className="site-icon site-icon-favicon" src={favicon} alt="" />;
  return (
    <span className="site-icon site-icon-mono" data-hue={monogramHue(registrableDomain(domain))} aria-hidden="true">
      {monogramLetter(domain)}
    </span>
  );
}

function open(event: React.MouseEvent, href: string, openUrl: (url: string) => Promise<void>) {
  // Modified clicks keep the browser's own behaviour; the plain click goes through the native bridge.
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
  event.preventDefault();
  void openUrl(href).catch(() => undefined);
}

export type LinkChipProps = { href: string; title?: string };

export function LinkChip({ href, title }: LinkChipProps) {
  const assets = useAssets();
  const domain = hostnameOf(href);
  if (!domain) return <span className="asset-plain">{title ?? href}</span>;
  return (
    <a className="chip link-chip" href={href} title={href} target="_blank" rel="noopener noreferrer" onClick={event => open(event, href, assets.openUrl)}>
      <SiteIcon domain={domain} />
      <span className="link-chip-title">{title || domain}</span>
      {title && <span className="link-chip-domain">{domain}</span>}
    </a>
  );
}

/** A link with its own words stays a text link; only a small site icon leads it. */
export function TextLink({ href, children }: { href: string; children: ReactNode }) {
  const assets = useAssets();
  const domain = hostnameOf(href);
  if (!domain) return <span>{children}</span>;
  return (
    <a className="text-link" href={href} title={href} target="_blank" rel="noopener noreferrer" onClick={event => open(event, href, assets.openUrl)}>
      <SiteIcon domain={domain} />
      {children}
    </a>
  );
}
