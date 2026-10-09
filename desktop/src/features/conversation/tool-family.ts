import type { IconName } from '../../components/ui';

const families: Array<[RegExp, IconName]> = [
  [/^(bash|shell|run|exec|terminal)/, 'terminal'],
  [/^(read|view|cat|open)/, 'file'],
  [/^(write|edit|patch|apply|replace)/, 'edit'],
  [/^(grep|glob|find|search|ls|list)/, 'findFiles'],
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

export function prettyArgs(args: string): string {
  try {
    return JSON.stringify(JSON.parse(args), null, 2);
  } catch {
    return args;
  }
}
