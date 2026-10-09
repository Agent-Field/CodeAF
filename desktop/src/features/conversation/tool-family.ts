import type { IconName } from '../../components/ui';

const families: Array<[RegExp, IconName]> = [
  [/^(bash|shell|run|exec|terminal)/, 'terminal'],
  [/^(read|view|cat|open)/, 'file'],
  [/^(write|edit|patch|apply|replace)/, 'edit'],
  [/^(ls|list|glob|find|dir|tree)/, 'folder'],
  [/^(grep|search|rg)/, 'findFiles'],
  [/^(web|fetch|browse|http)/, 'web'],
  [/^(task|propose_task|plan)/, 'tasks'],
];

export function toolIcon(tool: string): IconName {
  const name = tool.toLowerCase();
  const match = families.find(([pattern]) => pattern.test(name));
  return match ? match[1] : 'tool';
}

export function toolLabel(tool: string): string {
  return tool.replace(/_/g, ' ');
}

/** The engine's hint without its leading tool word: the row's icon and label
 * already name the tool. Empty when the hint says nothing else. */
export function hintText(tool: string, hint: string): string {
  const prefix = `${tool} `;
  if (!tool || !hint.startsWith(prefix)) return hint === tool ? '' : hint.trim();
  return hint.slice(prefix.length).trim();
}

/** A row's words: the tool word goes only when the row's icon already names it. */
export function rowHint(tool: string, hint: string): string {
  const named = toolIcon(tool) !== 'tool';
  return (named ? hintText(tool, hint) : hint) || toolLabel(tool);
}

/** True when the arguments say nothing the one-line hint does not: every value is text the hint already shows. */
export function argsRestateHint(args: string, hint: string): boolean {
  if (!args.trim()) return true;
  try {
    const values = Object.values(JSON.parse(args) as Record<string, unknown>);
    return values.every((value) => typeof value === 'string' && hint.includes(value));
  } catch {
    return false;
  }
}

export function prettyArgs(args: string): string {
  try {
    return JSON.stringify(JSON.parse(args), null, 2);
  } catch {
    return args;
  }
}
