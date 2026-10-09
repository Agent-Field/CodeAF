// Step titles and categories. Precedence is narration > engine Caption >
// composed from the tools (ported from tui3 caption.go composeCaption).

import type { ToolStep } from '../types.ts';
import { argString, argsOf, baseName, dirName, firstLine } from './args.ts';

const NARRATION_LIMIT = 90;

const CATEGORY_BY_TOOL: Record<string, string> = {
  bash: 'run',
  read: 'read',
  read_document: 'read',
  view_image: 'read',
  edit: 'edit',
  write: 'create',
  generate_image: 'create',
  generate_music: 'create',
  generate_video: 'create',
  speak: 'create',
  grep: 'search',
  find: 'search',
  ls: 'search',
  propose_task: 'plan',
  quick_task: 'plan',
  tasks: 'plan',
};

/** CaptionCategory when the engine gave one; else the tool's own family. */
export function categoryOf(tool: string, captionCategory?: string): string {
  if (captionCategory) return captionCategory;
  if (tool.startsWith('web_')) return 'browse';
  return CATEGORY_BY_TOOL[tool] ?? 'work';
}

/** The narration line as a title: first line, no marks, no closing stop. */
export function narrationTitle(text: string): string {
  const line = firstLine(text)
    .replace(/[*_`~]+/g, '')
    .replace(/[.!?;:]+$/, '')
    .trim();
  return line.length > NARRATION_LIMIT ? `${line.slice(0, NARRATION_LIMIT - 1).trimEnd()}…` : line;
}

type Verbs = [present: string, past: string];
const VERBS: Record<string, Verbs> = {
  read: ['Reading', 'Read'],
  edit: ['Editing', 'Edited'],
  write: ['Writing', 'Wrote'],
  bash: ['Running', 'Ran'],
  grep: ['Searching', 'Searched'],
  find: ['Searching', 'Searched'],
  ls: ['Listing', 'Listed'],
  web_search: ['Searching the web', 'Searched the web'],
  web_fetch: ['Fetching', 'Fetched'],
  generate_image: ['Generating', 'Generated'],
};

const BASH_THEMES: [RegExp, Verbs][] = [
  [/go test|make test|pytest|npm test|vitest|cargo test/, ['Running the tests', 'Ran the tests']],
  [/go build|make build|npm run build|cargo build/, ['Building', 'Built the project']],
  [/^git (status|diff|log|show)/, ['Checking git', 'Checked git']],
];

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const pick = (verbs: Verbs, done: boolean) => verbs[done ? 1 : 0];

function pathsOf(calls: ToolStep[]): string[] {
  const paths = calls.map((c) => argString(argsOf(c.args), 'path')).filter(Boolean);
  return [...new Set(paths)];
}

function sharedDir(paths: string[]): string {
  const dirs = new Set(paths.map(dirName));
  const [only] = dirs;
  return dirs.size === 1 && only ? only : '';
}

function filesTail(calls: ToolStep[]): string {
  const paths = pathsOf(calls);
  if (paths.length === 0) return plural(calls.length, 'file');
  if (paths.length === 1) return baseName(paths[0]);
  const dir = sharedDir(paths);
  return `${plural(paths.length, 'file')}${dir ? ` in ${dir}` : ''}`;
}

function bashPhrase(calls: ToolStep[], done: boolean): string {
  const command = calls.length === 1 ? argString(argsOf(calls[0].args), 'command').toLowerCase() : '';
  const theme = BASH_THEMES.find(([pattern]) => pattern.test(command));
  if (theme) return pick(theme[1], done);
  const tail = calls.length === 1 ? 'a command' : plural(calls.length, 'command');
  return `${pick(VERBS.bash, done)} ${tail}`;
}

function termOf(calls: ToolStep[], key: string): string {
  return calls.length === 1 ? argString(argsOf(calls[0].args), key) : '';
}

function searchTail(calls: ToolStep[], key: string): string {
  const term = termOf(calls, key);
  if (term) return `for “${term}”`;
  return calls.length === 1 ? 'the tree' : `${calls.length} times`;
}

function phraseFor(tool: string, calls: ToolStep[], done: boolean): string {
  const verb = VERBS[tool];
  if (!verb) return `${done ? 'Used' : 'Using'} ${tool.replace(/_/g, ' ')}`;
  if (tool === 'bash') return bashPhrase(calls, done);
  const lead = pick(verb, done);
  if (tool === 'read' || tool === 'edit' || tool === 'write') return `${lead} ${filesTail(calls)}`;
  if (tool === 'web_search') return `${lead}${termOf(calls, 'query') ? ` ${searchTail(calls, 'query')}` : ''}`;
  if (tool === 'grep' || tool === 'find') return `${lead} ${searchTail(calls, tool === 'grep' ? 'pattern' : 'name')}`;
  if (tool === 'generate_image') return `${lead} ${calls.length === 1 ? 'an image' : plural(calls.length, 'image')}`;
  if (tool === 'web_fetch') return `${lead} ${plural(calls.length, 'page')}`;
  return `${lead} ${plural(calls.length, 'item')}`;
}

function groupByTool(calls: ToolStep[]): Map<string, ToolStep[]> {
  const groups = new Map<string, ToolStep[]>();
  for (const call of calls) groups.set(call.tool, [...(groups.get(call.tool) ?? []), call]);
  return groups;
}

const lowerFirst = (text: string) => text.charAt(0).toLowerCase() + text.slice(1);

/** "Read 3 files in internal/x", "Ran 2 commands"; present tense until done. */
export function composedTitle(calls: ToolStep[], done: boolean): string {
  const phrases = [...groupByTool(calls)].map(([tool, group]) => phraseFor(tool, group, done));
  return phrases.map((phrase, i) => (i === 0 ? phrase : lowerFirst(phrase))).join(', ');
}
