// What a tool row points at and the number it wears (ELEMENTS §3.3). Pure.

import type { IconName } from '../../../components/ui/Icon';
import type { ToolStep } from '../types';
import { hintText, toolLabel } from '../tool-family.ts';
import { argPath, argString, bashExit, editStat, listCount, parseArgs, readLines, writeLines } from './stats.ts';

export type Target =
  | { kind: 'file'; path: string }
  | { kind: 'command'; text: string }
  | { kind: 'link'; url: string }
  | { kind: 'text'; text: string };

export type CallStat = { added?: number; removed?: number; capped?: boolean; text?: string };

const FILE_TOOLS = ['read', 'write', 'edit', 'view_image'];
const categoryIcons: Record<string, IconName> = {
  search: 'findFiles',
  read: 'file',
  edit: 'edit',
  create: 'create',
  run: 'terminal',
  test: 'test',
  browse: 'browse',
  transfer: 'transfer',
  communicate: 'communicate',
  coordinate: 'coordinate',
  plan: 'plan',
  wait: 'wait',
  work: 'tool',
};

export function categoryIcon(category: string): IconName {
  return categoryIcons[category] ?? 'tool';
}

function firstLine(text: string): string {
  return text.split('\n', 1)[0].trim();
}

function excerpt(text: string, limit = 80): string {
  const line = firstLine(text);
  return line.length > limit ? `${line.slice(0, limit)}…` : line;
}

function textTarget(call: ToolStep, ...keys: string[]): Target {
  const fields = parseArgs(call.args);
  const value = keys.map((key) => argString(fields, key)).find(Boolean);
  return { kind: 'text', text: value ? excerpt(value) : hintText(call.tool, call.hint) || toolLabel(call.tool) };
}

export function callTarget(call: ToolStep): Target {
  const fields = parseArgs(call.args);
  if (FILE_TOOLS.includes(call.tool)) {
    const path = argPath(call.args);
    if (path) return { kind: 'file', path };
  }
  switch (call.tool) {
    case 'bash':
      return { kind: 'command', text: firstLine(argString(fields, 'command') || hintText('bash', call.hint)) };
    case 'grep':
    case 'find':
      return textTarget(call, 'pattern', 'path');
    case 'ls':
      return textTarget(call, 'path');
    case 'web_search':
      return { kind: 'text', text: `“${excerpt(argString(fields, 'query') || hintText(call.tool, call.hint))}”` };
    case 'web_fetch':
      return argString(fields, 'url') ? { kind: 'link', url: argString(fields, 'url') } : textTarget(call);
    case 'generate_image':
      return textTarget(call, 'prompt');
    case 'propose_task':
    case 'quick_task':
    case 'tasks':
      return textTarget(call, 'title');
    default:
      return { kind: 'text', text: toolLabel(call.tool) };
  }
}

/** Plain words for a target: the toggle's accessible name and the fallback render. */
export function targetText(target: Target): string {
  if (target.kind === 'file') return target.path;
  return target.kind === 'link' ? target.url : target.text;
}

const lines = (count: number | undefined, noun = 'lines'): CallStat => (count === undefined ? {} : { text: `${count} ${noun}` });

export function callStat(call: ToolStep): CallStat {
  switch (call.tool) {
    case 'edit': {
      const { added, removed, capped } = editStat(call.args);
      return added + removed > 0 ? { added, removed, capped } : {};
    }
    case 'write': {
      const count = writeLines(call.args);
      return count === undefined ? {} : { added: count, text: 'lines' };
    }
    case 'read':
      return call.state === 'done' ? lines(readLines(call.output)) : {};
    case 'grep':
    case 'find':
      return call.state === 'done' ? lines(listCount(call.output), 'matches') : {};
    case 'ls':
      return call.state === 'done' ? lines(listCount(call.output), 'entries') : {};
    case 'bash': {
      const code = bashExit(call.output);
      return code === undefined ? {} : { text: `exit ${code}` };
    }
    default:
      return {};
  }
}
