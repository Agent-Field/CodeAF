// What the tab strip, hover preview and overview know about a conversation.
// Pure: built from the canonical snapshot, never from the view.

import type { EngineSnapshot } from '../chat/engine-client.ts';
import { digestOf } from './transcript-parse.ts';

export type TabMark = 'working' | 'waiting' | 'failed';

export type TabSummary = {
  title: string; // engine title, '' until generated
  firstLine: string; // first line of the first message, clipped for a tab label
  digest: string; // first line of the latest answer
  mark?: TabMark;
  updatedAt?: number;
};

const LABEL_LIMIT = 40;

export function labelLine(text: string): string {
  const line = text.split('\n').find((candidate) => candidate.trim()) ?? '';
  const trimmed = line.trim();
  if (trimmed.length <= LABEL_LIMIT) return trimmed;
  return `${trimmed.slice(0, LABEL_LIMIT - 1).trimEnd()}…`;
}

function markOf(snapshot: EngineSnapshot, failed: boolean): TabMark | undefined {
  if (snapshot.needsPerson || snapshot.questions?.length) return 'waiting';
  if (snapshot.running) return 'working';
  return failed ? 'failed' : undefined;
}

function lastAnswer(snapshot: EngineSnapshot): string {
  const reply = [...snapshot.entries].reverse().find((entry) => entry.Role === 'assistant' && entry.Text.trim());
  return reply ? digestOf(reply.Text) : '';
}

export function summarize(snapshot: EngineSnapshot, failed = false): TabSummary {
  const first = snapshot.entries.find((entry) => entry.Role === 'user');
  const stamp = Date.parse(snapshot.updatedAt ?? '');
  return {
    title: snapshot.title.trim(),
    firstLine: first ? labelLine(first.Text) : '',
    digest: lastAnswer(snapshot),
    mark: markOf(snapshot, failed),
    updatedAt: Number.isFinite(stamp) ? stamp : undefined,
  };
}

export function relativeTime(recordedAt: number, now: number): string {
  const seconds = Math.max(0, Math.floor((now - recordedAt) / 1000));
  if (seconds < 60) return 'just now';
  const format = new Intl.RelativeTimeFormat(undefined, { numeric: 'always' });
  if (seconds < 3600) return format.format(-Math.floor(seconds / 60), 'minute');
  if (seconds < 86400) return format.format(-Math.floor(seconds / 3600), 'hour');
  return format.format(-Math.floor(seconds / 86400), 'day');
}
