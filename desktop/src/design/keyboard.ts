/** Platform conventions shared by controls, menus and workspace hints. */
export const isMac = /Mac/.test(navigator.platform);
export function formatShortcut(label: string) {
 return label.replace('⌘/Ctrl', isMac ? '⌘' : 'Ctrl').replace('⇧', isMac ? '⇧' : 'Shift');
}
export const tabShortcuts = {
 new: formatShortcut('⌘/Ctrl T'),
 close: formatShortcut('⌘/Ctrl W'),
 reopen: formatShortcut('⌘/Ctrl ⇧ T'),
 /** Groups the active tab with the tabs picked by ⌘-click (Interactions, Shortcuts: "⌘G Group selected tabs"). */
 group: formatShortcut('⌘/Ctrl G'),
 /** Takes back the last structural tab action (Interactions, Shortcuts: "⌘Z Undo structural action"). */
 undo: formatShortcut('⌘/Ctrl Z'),
 switch: isMac ? '⌃ Tab' : 'Ctrl Tab',
 switchBack: isMac ? '⌃ ⇧ Tab' : 'Ctrl Shift Tab',
};
export const overviewShortcut = isMac ? '⌘ ⇧ \\' : 'Ctrl Shift A';
export const shellShortcuts = {
 rail: formatShortcut('⌘/Ctrl S'),
 focus: formatShortcut('⌘/Ctrl ⇧ F'),
 history: formatShortcut('⌘/Ctrl Y'),
 tasks: formatShortcut('⌘/Ctrl ⇧ K'),
 palette: formatShortcut('⌘/Ctrl K'),
 settings: formatShortcut('⌘/Ctrl ,'),
 openFile: formatShortcut('⌘/Ctrl O'),
 allModels: formatShortcut('⌘/Ctrl /'),
 /** The pinned-model chord for the 1-based slot (design Interactions: ⌥⌘1–3). */
 pinnedModel: (slot: number) => (isMac ? `⌥⌘${slot}` : `Ctrl Alt ${slot}`),
};

type KeyEvent = Pick<KeyboardEvent, 'key' | 'code' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'> & { target?: EventTarget | null };

/**
 * Every window-level shortcut of the shell, as ids. This is the ONE place that decides which chord means what,
 * so two surfaces can never both claim a key (design Interactions "Shortcuts"). `jump` carries the tab digit and
 * `model-pin` the pinned-model slot (⌥⌘1–3). ⌘⇧\ (Ctrl Shift A on Linux) is the tab overview; ⌘↑ and ⌘↓ step between
 * messages in a chat. ⌘/Ctrl B for the rail keeps working beside ⌘S. 'open-file' (⌘/Ctrl O) belongs to the new-tab field: only that surface claims it.
 */
export type ShortcutId =
 | 'new' | 'close' | 'reopen' | 'group' | 'undo' | 'next' | 'previous' | 'switch' | 'switch-back' | 'jump'
 | 'terminal' | 'open-file' | 'overview' | 'turn-previous' | 'turn-next' | 'history' | 'tasks' | 'rail' | 'focus' | 'palette' | 'models' | 'model-pin' | 'settings';
export type Shortcut = { id: ShortcutId; index?: number };

/** A text field with words in it keeps ⌘↑ and ⌘↓ as caret keys. */
function isWritingField(target: EventTarget | null | undefined): boolean {
 const field = target as { tagName?: string; value?: string } | null | undefined;
 return (field?.tagName === 'TEXTAREA' || field?.tagName === 'INPUT') && (field.value?.length ?? 0) > 0;
}

export function shortcutOf(event: KeyEvent, mac = isMac): Shortcut | undefined {
 if (event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey && event.code === 'Backquote') return { id: 'terminal' };
 if (event.ctrlKey && !event.metaKey && !event.altKey && event.key === 'Tab') return { id: event.shiftKey ? 'switch-back' : 'switch' };
 const primaryKey = mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
 // ⌥⌘1–3 pick a pinned model. The digit comes from `code`: Option turns the key into a symbol on a Mac.
 if (event.altKey) {
  const slot = /^(?:Digit|Numpad)([1-3])$/.exec(event.code ?? '');
  return primaryKey && !event.shiftKey && slot ? { id: 'model-pin', index: Number(slot[1]) } : undefined;
 }
 if (event.shiftKey && mac && event.metaKey && !event.ctrlKey) {
  if (event.code === 'Backslash') return { id: 'overview' };
  if (event.code === 'BracketRight') return { id: 'next' };
  if (event.code === 'BracketLeft') return { id: 'previous' };
 }
 if (event.shiftKey && !mac && event.ctrlKey && !event.metaKey && event.key.toLowerCase() === 'a') return { id: 'overview' };
 if (!mac && event.ctrlKey && !event.metaKey && !event.shiftKey) {
  if (event.key === 'PageDown') return { id: 'next' };
  if (event.key === 'PageUp') return { id: 'previous' };
 }
 if (!primaryKey) return;
 const key = event.key.toLowerCase();
 if (event.shiftKey) {
  if (key === 't') return { id: 'reopen' };
  if (key === 'k') return { id: 'tasks' };
  if (key === 'f') return { id: 'focus' };
  return;
 }
 if (key === 't') return { id: 'new' };
 if (key === 'w') return { id: 'close' };
 if (key === 'g') return { id: 'group' };
 // ⌘Z: the window's structural Undo (Interactions, Shortcuts). Its handler steps aside inside text fields and terminals.
 if (key === 'z') return { id: 'undo' };
 if (key === 'o') return { id: 'open-file' };
 if (key === 's' || key === 'b') return { id: 'rail' };
 if (key === 'y') return { id: 'history' };
 if (key === 'k') return { id: 'palette' };
 if (key === ',') return { id: 'settings' };
 if (key === '/') return { id: 'models' };
 if (/^[1-9]$/.test(key)) return { id: 'jump', index: Number(key) };
 if (event.key === 'ArrowUp' && !isWritingField(event.target)) return { id: 'turn-previous' };
 if (event.key === 'ArrowDown' && !isWritingField(event.target)) return { id: 'turn-next' };
}

/** A handler returns true when it used the shortcut; the registry then stops and cancels the browser's own. */
export type ShortcutHandler = (shortcut: Shortcut, event: KeyboardEvent) => boolean | void;
/** Higher layers see a chord first: the surface on screen (composer, turns), then the workspace, then the app. */
export const shortcutLayer = { surface: 30, workspace: 20, app: 10 } as const;

const handlers: { layer: number; handler: ShortcutHandler }[] = [];
function dispatchShortcut(event: KeyboardEvent) {
 const shortcut = shortcutOf(event);
 if (!shortcut) return;
 for (const { handler } of [...handlers]) {
  if (handler(shortcut, event)) { event.preventDefault(); event.stopPropagation(); return; }
 }
}
/** The one window listener. Handlers are tried layer by layer, newest first; returns the way to unregister. */
export function registerShortcuts(layer: number, handler: ShortcutHandler): () => void {
 const entry = { layer, handler };
 const at = handlers.findIndex(other => other.layer <= layer);
 handlers.splice(at === -1 ? handlers.length : at, 0, entry);
 if (handlers.length === 1) window.addEventListener('keydown', dispatchShortcut, true);
 return () => {
  handlers.splice(handlers.indexOf(entry), 1);
  if (handlers.length === 0) window.removeEventListener('keydown', dispatchShortcut, true);
 };
}

/** Composer: Enter sends (steers while running), Shift+Enter adds a line, Alt/Option+Enter queues while running. */
export const composerShortcuts = { queue: isMac ? '⌥↵' : 'Alt Enter' };
export function isWorkShortcut(event: KeyEvent): 'focus'|'preset'|undefined {
 const primary=isMac?event.metaKey&&!event.ctrlKey:event.ctrlKey&&!event.metaKey;
 if(!primary||event.altKey||event.shiftKey)return;
 if(event.key.toLowerCase()==='l')return 'focus';
 if(event.key==='.')return 'preset';
}

/** File and diff tabs: copy the open file's path (Shell 3e menu shows it beside Copy path). */
export const copyPathShortcut = formatShortcut('⌘/Ctrl ⇧ C');
export function isCopyPathShortcut(event: KeyEvent) {
 const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
 return primary && event.shiftKey && !event.altKey && event.key.toLowerCase() === 'c';
}

/** Interactive shell tabs: Control-backtick is identical on every platform. */
export const newTerminalShortcut = isMac ? '⌃`' : 'Ctrl `';
export const isNewTerminalShortcut = (event: KeyEvent) => shortcutOf(event)?.id === 'terminal';
