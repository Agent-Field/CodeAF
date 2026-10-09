// Text-level readers for the projection: the answer digest and the task
// completion aside. Pure string work, no engine types.

import type { TurnItem } from './types.ts';

const DIGEST_LIMIT = 140;
const TASK_STATUSES = ['done', 'failed', 'stopped', 'incomplete'];
const STATUS_TAIL = new RegExp(`^(.+)\\s(${TASK_STATUSES.join('|')})$`);
const META_SEGMENT = /^(ran|took|cost|used|\$|\d)/i;

type TaskItem = Extract<TurnItem, { kind: 'task' }>;
export type TaskAside = Omit<TaskItem, 'id' | 'taskId'>;

function stripMarks(line: string): string {
  return line
    .replace(/^\s*(?:#{1,6}\s+|>\s*|[-*+]\s+|\d+[.)]\s+)/, '')
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/[*_`~]+/g, '')
    .replace(/\s+/g, ' ')
    .trim();
}

function clip(text: string, limit: number): string {
  if (text.length <= limit) return text;
  return `${text.slice(0, limit - 1).trimEnd()}…`;
}

function isSkippable(raw: string): boolean {
  const rule = /^\s*([-*_]\s*){3,}$/.test(raw);
  const tableRule = /^\s*\|?[-:| ]+\|?\s*$/.test(raw) && raw.includes('-');
  return rule || tableRule;
}

export function digestOf(markdown: string): string {
  let fenced = false;
  for (const raw of markdown.split('\n')) {
    if (/^\s*(```|~~~)/.test(raw)) {
      fenced = !fenced;
      continue;
    }
    if (fenced || isSkippable(raw)) continue;
    const line = stripMarks(raw);
    if (line) return clip(line, DIGEST_LIMIT);
  }
  return '';
}

function firstSentence(text: string): string {
  const match = text.match(/^.*?[.!?](?=\s|$)/);
  return clip((match ? match[0] : text).trim(), DIGEST_LIMIT);
}

function asideOf(title: string, status: string, segments: string[], fallback: string, text: string): TaskAside {
  const rest = segments.filter((s) => !META_SEGMENT.test(s));
  return { kind: 'task', title, status, summary: firstSentence(rest[0] ?? fallback), body: text };
}

/** `<title> <status> · <summary> · …` on the first line. */
function inlineAside(lines: string[], text: string): TaskAside | undefined {
  const segments = lines[0].split(' · ').map((s) => s.trim());
  const head = segments[0].match(STATUS_TAIL);
  if (!head || segments.length < 2) return undefined;
  const next = lines.slice(1).find((l) => l.trim()) ?? '';
  return asideOf(head[1].trim(), head[2], segments.slice(1), next, text);
}

/** A run's report: the ask it came from, then a line `<status> · <summary> · …`. */
function reportAside(lines: string[], text: string): TaskAside | undefined {
  for (const line of lines.slice(1)) {
    const segments = line.split(' · ').map((s) => s.trim());
    if (segments.length > 1 && TASK_STATUSES.includes(segments[0])) {
      return asideOf(lines[0].trim(), segments[0], segments.slice(1), '', text);
    }
  }
  return undefined;
}

export function parseTaskAside(text: string): TaskAside | undefined {
  const lines = text.split('\n');
  return inlineAside(lines, text) ?? reportAside(lines, text);
}
