// What a turn made, promoted out of the work block (ELEMENTS §5.3-5.5):
// generated pictures and media sit before the final answer, one changes
// summary of the written files after the last answer.

import type { Deliverable, FileRef, ToolStep, TurnBlock } from '../types.ts';
import { argString, argsOf, firstLine } from './args.ts';
import { callStat } from './diff.ts';

const CAPTION_LIMIT = 80;
const SEPARATOR = ' — ';
const MEDIA_KIND: Record<string, 'audio' | 'video'> = {
  speak: 'audio',
  generate_music: 'audio',
  generate_video: 'video',
  edit_video: 'video',
};

const succeeded = (call: ToolStep) => call.state === 'done';

function clip(text: string): string {
  const line = firstLine(text);
  return line.length > CAPTION_LIMIT ? `${line.slice(0, CAPTION_LIMIT - 1).trimEnd()}…` : line;
}

/** `<path> — <facts>` is the shape generated-file results share. */
function resultParts(output: string): { path: string; facts: string } {
  const line = firstLine(output);
  const at = line.indexOf(SEPARATOR);
  if (at <= 0) return { path: '', facts: '' };
  return { path: line.slice(0, at).trim(), facts: line.slice(at + SEPARATOR.length).trim() };
}

/** "1024×1024 png, 1.4MB, generated on x" -> "1024×1024 png". */
function metaOf(facts: string): string {
  const size = /^(\d+\s*[×x]\s*\d+)\s+([a-z0-9]+)/i.exec(facts);
  return size ? `${size[1].replace(/\s+/g, '')} ${size[2]}` : '';
}

function pathOf(call: ToolStep): { path: string; facts: string } {
  const parts = resultParts(call.output);
  return { path: argString(argsOf(call.args), 'path') || parts.path, facts: parts.facts };
}

function imageOf(call: ToolStep): Deliverable | undefined {
  const { path, facts } = pathOf(call);
  if (!path) return undefined;
  const caption = clip(argString(argsOf(call.args), 'prompt'));
  return { kind: 'image', id: `${call.id}:image`, path, caption, meta: metaOf(facts) };
}

function mediaOf(call: ToolStep, media: 'audio' | 'video'): Deliverable | undefined {
  const { path } = pathOf(call);
  if (!path) return undefined;
  const caption = clip(argString(argsOf(call.args), 'prompt') || argString(argsOf(call.args), 'text'));
  return { kind: 'media', id: `${call.id}:media`, path, media, caption };
}

function producedBy(call: ToolStep): Deliverable | undefined {
  if (!succeeded(call)) return undefined;
  if (call.tool === 'generate_image') return imageOf(call);
  const media = MEDIA_KIND[call.tool];
  return media ? mediaOf(call, media) : undefined;
}

/** One FileRef per path, stats summed across the turn's calls. */
export function changedFiles(calls: ToolStep[]): FileRef[] {
  const files = new Map<string, FileRef>();
  for (const call of calls) {
    const path = argString(argsOf(call.args), 'path');
    if (!path || !succeeded(call) || (call.tool !== 'write' && call.tool !== 'edit')) continue;
    const stat = callStat(call.tool, call.args);
    const known = files.get(path);
    files.set(path, {
      path,
      added: (known?.added ?? 0) + stat.added,
      removed: (known?.removed ?? 0) + stat.removed,
      capped: known?.capped || stat.capped || undefined,
      source: known?.source === 'write' || call.tool === 'write' ? 'write' : 'edit',
    });
  }
  return [...files.values()];
}

/** The deliverables of a turn's calls, in call order. */
export function deliverablesOf(turnId: string, calls: ToolStep[]): { before: TurnBlock[]; after: TurnBlock[] } {
  const before = calls.flatMap((call) => {
    const deliverable = producedBy(call);
    return deliverable ? [{ kind: 'deliverable' as const, id: `${turnId}:${deliverable.id}`, deliverable }] : [];
  });
  const files = changedFiles(calls);
  const after: TurnBlock[] = files.length
    ? [{ kind: 'deliverable', id: `${turnId}:changes`, deliverable: { kind: 'changes', id: `${turnId}:changes`, files } }]
    : [];
  return { before, after };
}

/** Pictures and media land just before the final answer; changes close the turn. */
export function placeDeliverables(blocks: TurnBlock[], turnId: string, calls: ToolStep[]): TurnBlock[] {
  const { before, after } = deliverablesOf(turnId, calls);
  const finalAt = blocks.map((b) => b.kind).lastIndexOf('answer');
  const at = finalAt < 0 ? blocks.length : finalAt;
  return [...blocks.slice(0, at), ...before, ...blocks.slice(at), ...after];
}
