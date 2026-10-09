// What a codeaf link is: the one address a tab can be copied as and opened from outside the app. Pure (no React, no
// Tauri) so node tests drive it, and mirrored rule for rule by src-tauri/src/links.rs. Both read ./link-cases.json.
//
// A LINK NAMES A DURABLE, CANONICAL TARGET AND NOTHING ELSE. It carries the engine's conversation id (the folder that
// holds the transcript), a canonical task id, a workspace-RELATIVE path or an engine terminal id. It never carries a
// path on this disk, the bridge's per-attach session id, a token, a window label or a draft: the engine resolves the
// conversation id to its transcript itself, so a link cannot point codeaf at a file it did not already own.
import { chatIdFromSessionFile } from '../../places/client.ts';
import type { Pane, Tab } from '../types.ts';
import { routeTask } from '../view-state.ts';
import { focusedPane } from '../helpers.ts';

/** The scheme the desktop app registers with the operating system (tauri.conf.json `plugins.deep-link`). */
export const LINK_SCHEME = 'codeaf';
const PREFIX = `${LINK_SCHEME}://`;

/** The same numbers links.rs enforces. */
export const LINK_LIMITS = { link: 8192, id: 128, task: 256, path: 4096 } as const;

export type LinkTarget =
  | { kind: 'chat'; chatId: string; taskId?: string }
  | { kind: 'file' | 'diff'; chatId: string; path: string }
  | { kind: 'terminal'; chatId: string; terminalId: string };

/**
 * Why a link was refused. `malformed`: not a readable codeaf address at all. `unknown`: a codeaf address that names
 * nothing codeaf links to. `unsafe`: it names something, but with an id or path that could leave its place.
 */
export type LinkProblem = 'malformed' | 'unknown' | 'unsafe';
export type ParsedLink = { ok: true; target: LinkTarget; canonical: string } | { ok: false; reason: LinkProblem };

/** What a person reads when a link does not open. Nothing was opened in every case. */
export const linkProblemSentence: Record<LinkProblem, string> = {
  malformed: 'That codeaf link is damaged, so nothing was opened.',
  unknown: 'codeaf does not know what that link points to, so nothing was opened.',
  unsafe: 'That link points outside what codeaf can open, so nothing was opened.',
};

const ID = /^[A-Za-z0-9_-]+$/;
const isId = (text: string) => text.length <= LINK_LIMITS.id && ID.test(text);
const plain = (text: string) => !/[\u0000-\u001f\u007f-\u009f]/.test(text);
const utf8Length = (text: string) => new TextEncoder().encode(text).length;

/** A canonical task id: no separator, not the expanded-tasks stop (`#tasks`), not a dot name. */
function isTaskId(text: string): boolean {
  return text !== '' && [...text].length <= LINK_LIMITS.task && plain(text) && !/[\\/]/.test(text) && !text.startsWith('#') && text !== '.' && text !== '..';
}

/** A workspace-relative path that cannot climb out: no root, no drive, no backslash, no empty, `.` or `..` segment. */
export function isRelativePath(path: string): boolean {
  if (path === '' || utf8Length(path) > LINK_LIMITS.path || !plain(path) || path.includes('\\') || path.startsWith('/') || /^[A-Za-z]:/.test(path)) return false;
  return path.split('/').every(part => part !== '' && part !== '.' && part !== '..');
}

const encodePath = (path: string) => path.split('/').map(encodeURIComponent).join('/');

/** The one spelling of a target. `parseDeepLink(linkOf(t)).target` is `t`. */
export function linkOf(target: LinkTarget): string {
  const chat = encodeURIComponent(target.chatId);
  switch (target.kind) {
    case 'chat': return target.taskId ? `${PREFIX}chat/${chat}/task/${encodeURIComponent(target.taskId)}` : `${PREFIX}chat/${chat}`;
    case 'file':
    case 'diff': return `${PREFIX}${target.kind}/${chat}?path=${encodePath(target.path)}`;
    case 'terminal': return `${PREFIX}terminal/${chat}/${encodeURIComponent(target.terminalId)}`;
  }
}

const fail = (reason: LinkProblem): ParsedLink => ({ ok: false, reason });

function decode(text: string): string | undefined {
  try { return decodeURIComponent(text); } catch { return undefined; }
}

/** Reads a link from outside the app. Every refusal is a reason; nothing here guesses what a broken link meant. */
export function parseDeepLink(raw: unknown): ParsedLink {
  if (typeof raw !== 'string' || raw === '' || utf8Length(raw) > LINK_LIMITS.link || /[\s\u0000-\u001f\u007f-\u009f]/.test(raw)) return fail('malformed');
  if (raw.slice(0, PREFIX.length).toLowerCase() !== PREFIX || raw.includes('#')) return fail('malformed');
  const rest = raw.slice(PREFIX.length);
  const mark = rest.indexOf('?');
  const path = mark < 0 ? rest : rest.slice(0, mark);
  const query = mark < 0 ? undefined : rest.slice(mark + 1);
  const segments = path.split('/');
  const kind = segments[0];
  // The shape first: which kind, how many segments, which query. A wrong shape names nothing codeaf links to.
  const shaped =
    segments.every(segment => segment !== '') &&
    ((kind === 'chat' && query === undefined && (segments.length === 2 || (segments.length === 4 && segments[2] === 'task'))) ||
      ((kind === 'file' || kind === 'diff') && segments.length === 2 && query !== undefined && query.startsWith('path=') && !query.includes('&')) ||
      (kind === 'terminal' && query === undefined && segments.length === 3));
  if (!shaped) return fail('unknown');
  const parts = segments.map(decode);
  const value = query === undefined ? undefined : decode(query.slice('path='.length));
  if (parts.some(part => part === undefined) || (query !== undefined && value === undefined)) return fail('malformed');
  const [, chatId, third, fourth] = parts as string[];
  if (!isId(chatId)) return fail('unsafe');
  let target: LinkTarget;
  if (kind === 'chat') {
    if (fourth !== undefined && !isTaskId(fourth)) return fail('unsafe');
    target = fourth !== undefined ? { kind: 'chat', chatId, taskId: fourth } : { kind: 'chat', chatId };
  } else if (kind === 'terminal') {
    if (!isId(third)) return fail('unsafe');
    target = { kind: 'terminal', chatId, terminalId: third };
  } else {
    if (!isRelativePath(value!)) return fail('unsafe');
    target = { kind: kind as 'file' | 'diff', chatId, path: value! };
  }
  return { ok: true, target, canonical: linkOf(target) };
}

/** A web page's address as it is copied: the parsed, normalized http(s) URL, never one carrying a user or password. */
export function webLinkOf(url: string | undefined): string | undefined {
  if (!url || url.length > LINK_LIMITS.link) return undefined;
  let parsed: URL;
  try { parsed = new URL(url); } catch { return undefined; }
  if ((parsed.protocol !== 'https:' && parsed.protocol !== 'http:') || !parsed.hostname || parsed.username || parsed.password) return undefined;
  const href = parsed.href;
  return href.length <= LINK_LIMITS.link && !/[\s\u0000-\u001f\u007f]/.test(href) ? href : undefined;
}

/** The conversation id behind a pane, or undefined when the pane holds no saved conversation. */
function chatOf(pane: Pane): string | undefined {
  const id = pane.sessionFile ? chatIdFromSessionFile(pane.sessionFile) : '';
  return isId(id) ? id : undefined;
}

/** The durable target a pane shows, or undefined when it shows nothing that outlives it. */
export function targetOf(pane: Pane): LinkTarget | undefined {
  const chatId = chatOf(pane);
  switch (pane.kind) {
    case 'conversation':
    case 'task': {
      if (!chatId) return undefined;
      const taskId = pane.route ? routeTask(pane.route) : undefined;
      return taskId && isTaskId(taskId) ? { kind: 'chat', chatId, taskId } : { kind: 'chat', chatId };
    }
    case 'file':
    case 'diff': {
      const path = pane.file?.path ?? pane.target?.path ?? pane.path;
      return chatId && path && isRelativePath(path) ? { kind: pane.kind, chatId, path } : undefined;
    }
    case 'terminal': {
      const terminalId = pane.target?.terminalId;
      return chatId && terminalId && isId(terminalId) ? { kind: 'terminal', chatId, terminalId } : undefined;
    }
    default:
      return undefined;
  }
}

/**
 * What "Copy link" copies for one pane: a codeaf link to its durable target, a web page's own address, or undefined
 * (a new tab, Settings, History, the Inbox, a conversation that was never sent). Undefined means the menu leaves
 * Copy link OFF: there is nothing honest to copy, and an absolute path is not a link.
 */
export function linkForPane(pane: Pane): string | undefined {
  if (pane.kind === 'web') return webLinkOf(pane.target?.url);
  const target = targetOf(pane);
  return target ? linkOf(target) : undefined;
}

/** A split tab copies the pane that has focus, the one the person is looking at. */
export const linkForTab = (tab: Tab): string | undefined => (tab.kind === 'inbox' ? undefined : linkForPane(focusedPane(tab)));
