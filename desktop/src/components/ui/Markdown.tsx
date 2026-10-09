import ReactMarkdown, { defaultUrlTransform } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Icon } from './Icon';
import { CodeText } from './Typography';
import { CopyButton } from './CopyButton';
import { useMoreToRight } from './useMoreToRight';
import type { ReactElement, ReactNode } from 'react';
import '../../styles/markdown.css';

/** A hook returns a node to take over an element, or undefined to keep the default rendering. */
export type MarkdownHooks = {
 renderLink?: (href: string, children: ReactNode) => ReactNode | undefined;
 renderInlineCode?: (text: string) => ReactNode | undefined;
 renderImage?: (src: string, alt: string) => ReactNode | undefined;
};
export type MarkdownProps = MarkdownHooks & { children: string; className?: string; onOpenLink?: (url: string) => void };
// A reference with no scheme may name a workspace file; only a hook can resolve it, so it passes through only when one is installed.
const hasScheme = /^[a-z][a-z0-9+.-]*:/i;
function localReference(value: string): string {
 const text = value.trim();
 return text && !text.startsWith('#') && !text.startsWith('//') && !hasScheme.test(text) && !/[\0\n\r]/.test(text) ? text : '';
}
/** AI output is content, never executable HTML or an arbitrary URL scheme. */
export function safeMarkdownUrl(value: string): string {
 const safe = defaultUrlTransform(value.trim());
 if (!safe) return '';
 if (safe.startsWith('#')) return safe;
 try {
  const parsed = new URL(safe);
  return ['http:', 'https:', 'mailto:'].includes(parsed.protocol) ? safe : '';
 } catch { return ''; }
}
function textOf(node: ReactNode): string {
 if (typeof node === 'string' || typeof node === 'number') return String(node);
 if (Array.isArray(node)) return node.map(textOf).join('');
 return '';
}
function CodeBlock({ children }: { children?: ReactNode }) {
 const code = children as ReactElement<{ className?: string; children?: ReactNode }> | undefined;
 const language = /language-([\w+#.-]+)/.exec(code?.props?.className ?? '')?.[1];
 return <div className="markdown-code">
  <div className="markdown-code-head">
   <span className="markdown-code-lang">{language}</span>
   <CopyButton text={textOf(code?.props?.children)} label="Copy code"/>
  </div>
  <pre>{children}</pre>
 </div>;
}
/** Tables keep real table semantics and a named keyboard-scrollable viewport; the right edge fades only while more lies that way. */
function Table({ children }: { children?: ReactNode }) {
 const { ref, more, measure } = useMoreToRight();
 return <div ref={ref} className="markdown-table-scroll" role="region" aria-label="Response table" tabIndex={0} data-more={more} onScroll={measure}><table>{children}</table></div>;
}
export function Markdown({ children, className = '', onOpenLink, renderLink, renderInlineCode, renderImage }: MarkdownProps) {
 const resolvesLocal = !!(renderLink || renderImage);
 const transform = (value: string) => safeMarkdownUrl(value) || (resolvesLocal ? localReference(value) : '');
 return <div className={`markdown ${className}`}><ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml urlTransform={transform} components={{
  pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
  code: ({ children, className }) => {
   // Fenced code carries a language or ends in a newline; anything else is an inline span a hook may enrich.
   const text = textOf(children);
   const inline = !className && !text.endsWith('\n');
   const hooked = inline && renderInlineCode ? renderInlineCode(text) : undefined;
   return hooked !== undefined ? <>{hooked}</> : <CodeText className={className}>{children}</CodeText>;
  },
  // Keep the scroller mounted when a live conversation renders again.
  table: Table,
  // GFM checklist marks describe output; they never act as approval controls.
  input: ({ checked }) => <span className="markdown-task-check" role="img" aria-label={checked ? 'Completed item' : 'Incomplete item'} data-checked={!!checked}>{checked && <Icon name="check" size="xs"/>}</span>,
  th: ({ children, style }) => <th scope="col" data-align={style?.textAlign}>{children}</th>,
  td: ({ children, style }) => <td data-align={style?.textAlign}>{children}</td>,
  a: ({ href, children }) => {
   const hooked = href && renderLink ? renderLink(href, children) : undefined;
   if (hooked !== undefined) return <>{hooked}</>;
   return !href || !safeMarkdownUrl(href) ? <span>{children}</span> : <a href={href} target={href.startsWith('#') ? undefined : '_blank'} rel={href.startsWith('#') ? undefined : 'noopener noreferrer'} onClick={event => { if (!href.startsWith('#') && onOpenLink) { event.preventDefault(); onOpenLink(href); } }}>{children}</a>;
  },
  img: ({ src, alt }) => {
   const hooked = typeof src === 'string' && renderImage ? renderImage(src, alt ?? '') : undefined;
   if (hooked !== undefined) return <>{hooked}</>;
   const href = typeof src === 'string' ? safeMarkdownUrl(src) : '';
   const label = alt ? `Image: ${alt}` : 'Referenced image';
   return href ? <a href={href} target="_blank" rel="noopener noreferrer" onClick={event => { if (onOpenLink) { event.preventDefault(); onOpenLink(href); } }}>{label}</a> : <span>{label}</span>;
  },
 }}>{children}</ReactMarkdown></div>;
}

/** Inline content keeps its surrounding heading or label typography and never creates block controls. */
export function InlineMarkdown({ children }: { children: string }) {
 return <ReactMarkdown skipHtml allowedElements={['p', 'strong', 'em', 'del', 'code']} unwrapDisallowed components={{
  p: ({ children }) => <>{children}</>,
  code: ({ children }) => <CodeText className="inline-code">{children}</CodeText>,
 }}>{children}</ReactMarkdown>;
}
