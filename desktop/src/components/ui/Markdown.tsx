import ReactMarkdown, { defaultUrlTransform } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Icon } from './Icon';
import { CodeText } from './Typography';
import '../../styles/markdown.css';

export type MarkdownProps = { children: string; className?: string; onOpenLink?: (url: string) => void };
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
export function Markdown({ children, className = '', onOpenLink }: MarkdownProps) {
 return <div className={`markdown ${className}`}><ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml urlTransform={safeMarkdownUrl} components={{
  code: ({ children, className }) => <CodeText className={className}>{children}</CodeText>,
  // Tables keep real table semantics and a named keyboard-scrollable viewport.
  table: ({ children }) => <div className="markdown-table-scroll" role="region" aria-label="Response table" tabIndex={0}><table>{children}</table></div>,
  // GFM checklist marks describe output; they never act as approval controls.
  input: ({ checked }) => <span className="markdown-task-check" role="img" aria-label={checked ? 'Completed item' : 'Incomplete item'} data-checked={!!checked}>{checked && <Icon name="check" size="xs"/>}</span>,
  th: ({ children, style }) => <th scope="col" data-align={style?.textAlign}>{children}</th>,
  td: ({ children, style }) => <td data-align={style?.textAlign}>{children}</td>,
  a: ({ href, children }) => !href ? <span>{children}</span> : <a href={href} target={href.startsWith('#') ? undefined : '_blank'} rel={href.startsWith('#') ? undefined : 'noopener noreferrer'} onClick={event => { if (!href.startsWith('#') && onOpenLink) { event.preventDefault(); onOpenLink(href); } }}>{children}</a>,
  img: ({ src, alt }) => {
   const href = typeof src === 'string' ? safeMarkdownUrl(src) : '';
   const label = alt ? `Image: ${alt}` : 'Referenced image';
   return href ? <a href={href} target="_blank" rel="noopener noreferrer" onClick={event => { if (onOpenLink) { event.preventDefault(); onOpenLink(href); } }}>{label}</a> : <span>{label}</span>;
  },
 }}>{children}</ReactMarkdown></div>;
}
