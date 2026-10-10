import { useEffect, useRef } from 'react';
import type { EngineSnapshot } from '../chat/engine-client.ts';
import { chatIdFromSessionFile } from '../places/client.ts';
import type { WorldRow } from '../world/types.ts';
import { useWorld } from '../world/useWorld.ts';
import type { WorldClient } from '../world/worldClient.ts';
import { summarize, type TabSummary } from './tabSummary.ts';

export type SessionTarget = { id: string; sessionFile: string };

/**
 * Facts a world row carries that an engine snapshot does not. They ride beside
 * the snapshot the hook already hands its caller, so the exported callback
 * stays `(id, snapshot)`.
 */
type RowFacts = { tasksRunning: number; failedCount: number; chatId: string };

const facts = new WeakMap<EngineSnapshot, RowFacts>();
const absent = 'absent';
const emptyUsage = { Input: 0, Output: 0, CostUSD: 0, Duration: 0, Turns: 0 };

/** The window's world rows. One function identity, so the store snapshot stays stable. */
function selectRows(world: WorldClient): readonly WorldRow[] {
  return world.rows();
}

/**
 * The row for a background tab. The journal path is the join the tab already
 * holds; the chat id is the fallback for a row that named the folder and not
 * the file. A bare test filename has no folder, so it only matches a row that
 * repeats that filename.
 */
export function rowForTarget(rows: readonly WorldRow[], target: SessionTarget): WorldRow | undefined {
  const byFile = rows.find(row => row.sessionFile === target.sessionFile);
  if (byFile) return byFile;
  const chatId = chatIdFromSessionFile(target.sessionFile);
  if (!chatId) return undefined;
  return rows.find(row => row.chatId === chatId);
}

/** The mock's session mirror rides on the row. The engine's own rows do not have these. */
type SessionCarry = { sessionId?: string; sessionSeq?: number; questions?: EngineSnapshot['questions'] };
const carry = (row: WorldRow): SessionCarry => row as WorldRow & SessionCarry;

function signatureOf(row: WorldRow): string {
  const extra = carry(row);
  const asked = extra.questions?.map(question => `${question.id}:${question.head}`).join('\n') ?? '';
  return JSON.stringify([row.running, row.needsYou, row.failed, row.tasksRunning, row.title ?? '', row.updatedAt ?? '', row.chatId, extra.sessionSeq ?? 0, asked]);
}

function blank(sessionFile: string): EngineSnapshot {
  return {
    id: '', sessionFile, workspace: '', model: '', persistent: true,
    running: false, needsPerson: false, entries: [], tasks: [], usage: emptyUsage,
    title: '', seq: 0, questions: [],
  };
}

/** A snapshot whose running, needs-you and title are the row's, and whose transcript is empty. */
function snapshotFromRow(target: SessionTarget, row: WorldRow): EngineSnapshot {
  const extra = carry(row);
  const snapshot: EngineSnapshot = {
    ...blank(target.sessionFile),
    id: extra.sessionId ?? '',
    seq: extra.sessionSeq ?? 0,
    workspace: row.workspace ?? '',
    // A task still running is work in flight even when the turn flag is down.
    running: row.running || row.tasksRunning > 0,
    needsPerson: row.needsYou > 0,
    title: row.title ?? '',
    updatedAt: row.updatedAt,
    questions: extra.questions ?? [],
  };
  facts.set(snapshot, { tasksRunning: row.tasksRunning, failedCount: row.failed, chatId: row.chatId });
  return snapshot;
}

/** The conversation left the world. Clear the live marks; the caller keeps any reply it already read. */
function snapshotGone(target: SessionTarget): EngineSnapshot {
  const snapshot = blank(target.sessionFile);
  facts.set(snapshot, { tasksRunning: 0, failedCount: 0, chatId: '' });
  return snapshot;
}

export type BackgroundUpdate = { id: string; snapshot: EngineSnapshot; signature: string };

/**
 * What changed for these tabs since `delivered` (tab id to the last signature).
 * A tab with no row yet is left alone: unknown is not the same as idle, and a
 * reply read while the tab was open must stay until a row actually speaks.
 * Once a row has spoken, its leaving clears the live marks.
 */
export function updatesFromWorldRows(targets: readonly SessionTarget[], rows: readonly WorldRow[], delivered: ReadonlyMap<string, string>): BackgroundUpdate[] {
  const updates: BackgroundUpdate[] = [];
  for (const target of targets) {
    const row = rowForTarget(rows, target);
    if (!row) {
      if (!delivered.has(target.id) || delivered.get(target.id) === absent) continue;
      updates.push({ id: target.id, snapshot: snapshotGone(target), signature: absent });
      continue;
    }
    const signature = signatureOf(row);
    if (delivered.get(target.id) === signature) continue;
    updates.push({ id: target.id, snapshot: snapshotFromRow(target, row), signature });
  }
  return updates;
}

/**
 * Folds a world-fed snapshot onto the summary already on the tab.
 * The row has a title, a running flag and a needs-you count. Question text stays
 * when the row did not bring its own; a row that did (the session mirror) replaces it.
 * Needs-you outranks running, and running outranks a landed failure — the same
 * order a full snapshot uses. A failure lights the mark only when nothing is
 * running and nothing is waiting.
 */
export function mergeBackgroundSummary(previous: TabSummary | undefined, snapshot: EngineSnapshot): TabSummary {
  const next = summarize(snapshot);
  const row = facts.get(snapshot);
  const failedCount = row?.failedCount ?? 0;
  const tasksRunning = row?.tasksRunning ?? next.running ?? 0;
  const mark = next.mark ?? (failedCount > 0 ? 'failed' : undefined);
  const chatId = row?.chatId || next.chatId;
  if (!previous) return { ...next, mark, running: tasksRunning, chatId, sessionId: next.sessionId || undefined };
  return {
    ...previous,
    title: next.title || previous.title,
    mark,
    running: tasksRunning,
    updatedAt: next.updatedAt ?? previous.updatedAt,
    chatId: chatId || previous.chatId,
    sessionId: snapshot.id || previous.sessionId,
    questions: snapshot.questions?.length ? snapshot.questions : (snapshot.needsPerson ? previous.questions : []),
  };
}

/**
 * Inactive tabs take later running, needs-you and question changes from the
 * world stream. They do not hold a conversation stream, and they do not attach:
 * a browser allows six connections to one host, and a stream per saved tab left
 * none for a new tab's first send. Only the open tab streams its own session.
 * A snapshot older than one already applied is dropped, so a late record cannot
 * wipe a newer one.
 */
export function useBackgroundSessions(targets: SessionTarget[], onSnapshot: (id: string, snapshot: EngineSnapshot) => void) {
  const rows = useWorld(selectRows);
  const receive = useRef(onSnapshot);
  receive.current = onSnapshot;
  const delivered = useRef(new Map<string, string>());
  const appliedSeq = useRef(new Map<string, number>());
  // The array is new every render; the key is the same while the set of tabs is.
  const key = JSON.stringify(targets);
  const deliver = (id: string, snapshot: EngineSnapshot) => {
    const seq = snapshot.seq ?? 0;
    const seen = appliedSeq.current.get(id);
    if (seen !== undefined && seq < seen) return;
    appliedSeq.current.set(id, seq);
    receive.current(id, snapshot);
  };

  useEffect(() => {
    const seen = delivered.current;
    const wanted = new Set(targets.map(target => target.id));
    for (const id of seen.keys()) if (!wanted.has(id)) { seen.delete(id); appliedSeq.current.delete(id); }
    for (const update of updatesFromWorldRows(targets, rows, seen)) {
      seen.set(update.id, update.signature);
      deliver(update.id, update.snapshot);
    }
  }, [rows, key]);
}
