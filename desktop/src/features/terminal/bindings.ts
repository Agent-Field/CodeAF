// Which engine terminal a terminal tab shows. A tab's pane carries no field for it (the tab model has
// one slot for a conversation's session file), so the binding lives here, keyed by pane id, and is
// validated on read like every other persisted piece of the workspace.

/** `sessionFile` names the conversation the terminal runs under; `terminalId` the engine terminal.
 * `refused` is the engine's own sentence when the terminal could not start (Q6: the limit line). */
export type TerminalBinding = { sessionFile?: string; terminalId?: string; refused?: string };

export const bindingsKey = 'codeaf.desktop.terminals.v1';

const text = (value: unknown): value is string => typeof value === 'string' && value !== '';

/** Keeps only the entries that validate; anything else is dropped rather than discarding the lot. */
export function parseBindings(raw: unknown): Record<string, TerminalBinding> {
  const out: Record<string, TerminalBinding> = {};
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return out;
  for (const [paneId, value] of Object.entries(raw as Record<string, unknown>)) {
    const entry = value as Partial<TerminalBinding> | null;
    if (!entry || typeof entry !== 'object') continue;
    const binding: TerminalBinding = {};
    if (text(entry.sessionFile)) binding.sessionFile = entry.sessionFile;
    if (text(entry.terminalId)) binding.terminalId = entry.terminalId;
    if (text(entry.refused)) binding.refused = entry.refused;
    if (binding.terminalId || binding.refused) out[paneId] = binding;
  }
  return out;
}

function read(): Record<string, TerminalBinding> {
  try { return parseBindings(JSON.parse(localStorage.getItem(bindingsKey) ?? 'null')); } catch { return {}; }
}
function write(all: Record<string, TerminalBinding>) {
  try { localStorage.setItem(bindingsKey, JSON.stringify(all)); } catch { /* A full or unavailable store only costs the tab its terminal after a reload. */ }
}

export const bindingOf = (paneId: string): TerminalBinding | undefined => read()[paneId];
export function bind(paneId: string, binding: TerminalBinding) { write({ ...read(), [paneId]: binding }); }
export function unbind(paneId: string) { const all = read(); if (paneId in all) { delete all[paneId]; write(all); } }
