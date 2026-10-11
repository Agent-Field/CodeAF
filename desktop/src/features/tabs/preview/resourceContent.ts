import type { EngineFileDiff, EngineTextFile, TerminalInfo } from '../../chat/engine-client.ts';
import { changedLines, headLines, tailLines, type FieldLine } from './content.ts';
import { loadedCache, resourceKey } from './loadedCache.ts';

export const previewTailBytes = 4096;
const compactLines = (lines: FieldLine[]) => lines.map(line => ({ ...line, text: line.text.slice(0, previewTailBytes) }));

export type FileContent = { lines: number; head: FieldLine[]; refusal?: string };
export type DiffContent = { added: number; deleted: number; head: FieldLine[] };
export type TerminalContent = { info: TerminalInfo; tail: FieldLine[] };

/** Live workspace reads prime only the small piece the preview needs, without retaining whole files or logs. */
export function rememberFile(session: string, path: string, file: EngineTextFile): FileContent {
  const content = { lines: file.lines, head: compactLines(headLines(file.text ?? '')), refusal: file.refusal ? file.message : undefined };
  loadedCache.seed(resourceKey('file', session, path), content);
  return content;
}
export function rememberDiff(session: string, path: string, diff: EngineFileDiff): DiffContent {
  const content = { added: diff.added, deleted: diff.deleted, head: compactLines(changedLines(diff)) };
  loadedCache.seed(resourceKey('diff', session, path), content);
  return content;
}
export function rememberTerminal(session: string, terminal: string, output: { info: TerminalInfo; text: string }): TerminalContent {
  const content = { info: output.info, tail: compactLines(tailLines(output.text)) };
  loadedCache.seed(resourceKey('terminal', session, terminal), content);
  return content;
}

/** Saved panes resolve the session already attached by their workspace instead of attaching another one. */
export function rememberPreviewSession(file: string, session: string) {
  if (file) loadedCache.seed(resourceKey('session', file, ''), session);
}
export function previewSession(file: string | undefined): string | undefined {
  return file ? loadedCache.read<string>(resourceKey('session', file, '')) : undefined;
}
