import type { MouseEvent, ReactNode } from 'react';
import { ContextMenu, useTooltip } from '../../../components/ui';
import { nativeWebAvailable } from '../../../design/nativeWeb';
import { isMac } from '../../../design/keyboard';
import { openWebTab } from '../../web/open';
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

function open(event: MouseEvent, href: string, openUrl: (url: string) => Promise<void>) {
  const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  const webGesture = event.button === 1 || (event.button === 0 && primary && !event.shiftKey && !event.altKey);
  if (webGesture) {
    event.preventDefault();
    // The native capability is checked before dispatch so browser builds never create an unusable web tab.
    if (nativeWebAvailable() && openWebTab(href)) return;
    void openUrl(href).catch(() => undefined);
    return;
  }
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
  event.preventDefault();
  void openUrl(href).catch(() => undefined);
}

/** Both chip and prose links share the URL actions so their labels never change where a gesture goes. */
function UrlLink({ href, className, children }: { href: string; className: string; children: ReactNode }) {
  const assets = useAssets();
  const tooltip = useTooltip<HTMLAnchorElement>(href, {}, { describe: true });
  return (
    <>
      <ContextMenu label="Link actions" items={[{
        id: 'copy-link', label: 'Copy link', icon: 'copy',
        onSelect: () => void navigator.clipboard.writeText(href).catch(() => undefined),
      }]}>
        <a className={className} href={href} target="_blank" rel="noopener noreferrer" {...tooltip.props}
          onClick={event => open(event, href, assets.openUrl)}
          onAuxClick={event => { if (event.button === 1) open(event, href, assets.openUrl); }}>
          {children}
        </a>
      </ContextMenu>
      {tooltip.element}
    </>
  );
}

export type LinkChipProps = { href: string; title?: string };

export function LinkChip({ href, title }: LinkChipProps) {
  const domain = hostnameOf(href);
  if (!domain) return <span className="asset-plain">{title ?? href}</span>;
  return (
    <UrlLink className="chip link-chip" href={href}>
      <SiteIcon domain={domain} />
      <span className="link-chip-title">{title || domain}</span>
      {title && <span className="link-chip-domain">{domain}</span>}
    </UrlLink>
  );
}

/** A link with its own words stays a text link; only a small site icon leads it. */
export function TextLink({ href, children }: { href: string; children: ReactNode }) {
  const domain = hostnameOf(href);
  if (!domain) return <span>{children}</span>;
  return (
    <UrlLink className="text-link" href={href}>
      <SiteIcon domain={domain} />
      {children}
    </UrlLink>
  );
}
