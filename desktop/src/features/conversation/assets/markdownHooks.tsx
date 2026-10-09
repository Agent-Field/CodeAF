import { useMemo, type ReactNode } from 'react';
import { CodeText, type MarkdownHooks } from '../../../components/ui';
import { useAssets, usePathFact } from './AssetContext';
import { FileChip } from './FileChip';
import { ImageFigure } from './ImageFigure';
import { LinkChip, TextLink } from './LinkChip';
import { hostnameOf, localPathFromHref, looksLikePath, relativePath } from './paths';

function textOf(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(textOf).join('');
  return '';
}

/** Keeps `fallback` until the engine confirms the path exists inside the workspace: no guessing. */
function WhenInWorkspace({ path, fallback, children }: { path: string; fallback: ReactNode; children: (path: string) => ReactNode }) {
  const assets = useAssets();
  const { fact } = usePathFact(path);
  const inside = fact?.exists && !fact.outside && relativePath(path, assets.workspace) !== null;
  return <>{inside ? children(path) : fallback}</>;
}

function link(href: string, children: ReactNode): ReactNode | undefined {
  if (hostnameOf(href)) {
    const words = textOf(children).trim();
    // An autolinked bare URL reads as its own address; that is the chip case.
    return !words || words === href || words === href.replace(/\/$/, '') ? <LinkChip href={href} /> : <TextLink href={href}>{children}</TextLink>;
  }
  const path = localPathFromHref(href);
  if (!path) return undefined;
  return (
    <WhenInWorkspace path={path} fallback={<span>{children}</span>}>
      {found => <FileChip path={found} source="markdown" />}
    </WhenInWorkspace>
  );
}

function inlineCode(text: string): ReactNode | undefined {
  if (!looksLikePath(text)) return undefined;
  return (
    <WhenInWorkspace path={text} fallback={<CodeText>{text}</CodeText>}>
      {found => <FileChip path={found} source="markdown" />}
    </WhenInWorkspace>
  );
}

function image(src: string, alt: string): ReactNode | undefined {
  const label = alt ? `Image: ${alt}` : 'Referenced image';
  // Remote pictures are never fetched by the renderer; they stay a link.
  if (hostnameOf(src)) return <LinkChip href={src} title={label} />;
  const path = localPathFromHref(src);
  if (!path) return undefined;
  return (
    <WhenInWorkspace path={path} fallback={<span>{label}</span>}>
      {found => <ImageFigure path={found} caption={alt} meta="" />}
    </WhenInWorkspace>
  );
}

/** Spread into <Markdown>: links, inline code and images become asset chips when an engine is attached. */
export function useAssetMarkdownHooks(): MarkdownHooks {
  const { available } = useAssets();
  return useMemo(() => (available ? { renderLink: link, renderInlineCode: inlineCode, renderImage: image } : {}), [available]);
}
