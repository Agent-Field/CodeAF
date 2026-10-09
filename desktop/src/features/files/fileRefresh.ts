/** How long a burst of writes waits before the open tab reads the file again. One read, not a poll. */
export const fileRefreshDelay = 1000;

/**
 * How often an open, visible file asks the engine for its size and modification time. One stat of one
 * path; a hidden tab, a hidden window and a closed tab ask nothing.
 */
export const fileStatInterval = 2000;

/** The version of a file as the engine's stat sees it. Equal versions are the same bytes as far as a stat can tell. */
export function fileVersion(fact: { exists: boolean; size: number; modTime?: string }): string {
  return fact.exists ? `${fact.size}\0${fact.modTime ?? ''}` : 'gone';
}

const fileTools = new Set(['edit', 'write', 'multiedit', 'apply_patch']);

const norm = (value: string) => value.replace(/\\/g, '/').replace(/^\.\//, '').replace(/\/+$/, '');

/**
 * True when both strings name the same workspace file. Only an absolute path may end in the other's
 * relative one: two relative paths are the same file only when they are equal, so an edit of `cmd/a.go`
 * does not refresh a tab showing the root `a.go`.
 */
export function sameWorkspaceFile(eventPath: string, openPath: string): boolean {
  const a = norm(eventPath);
  const b = norm(openPath);
  if (!a || !b) return false;
  if (a === b) return true;
  return (a.startsWith('/') && !b.startsWith('/') && a.endsWith('/' + b)) || (b.startsWith('/') && !a.startsWith('/') && b.endsWith('/' + a));
}

/** The path a file tool named. Malformed arguments are not a path. */
export function toolArgsPath(args: string): string {
  try {
    const parsed = JSON.parse(args) as { path?: unknown; file_path?: unknown; filePath?: unknown };
    for (const value of [parsed.path, parsed.file_path, parsed.filePath]) {
      if (typeof value === 'string' && value.trim()) return value;
    }
  } catch {
    /* A tool that did not send JSON did not name a file. */
  }
  return '';
}

/** An edit or write whose arguments point at the open file. A shell command does not, because it names no file. */
export function fileEventTouches(tool: string, args: string, openPath: string): boolean {
  return fileTools.has(tool) && sameWorkspaceFile(toolArgsPath(args), openPath);
}
