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
export const placeShortcuts = {
 goTo: formatShortcut('⌘/Ctrl P'),
 allPlaces: isMac ? '⌘⇧P' : 'Ctrl Shift P',
 home: formatShortcut('⌘/Ctrl 0'),
 close: isMac ? '⌘⇧W' : 'Ctrl Shift W',
 newWindow: formatShortcut('⌘/Ctrl N'),
 /** The rail slot chord: ⌃1 on a Mac, Alt 1 elsewhere; 0 is Now. */
 slot: (index: number) => (isMac ? `⌃${index}` : `Alt ${index}`),
 /** ⌘↵ / Ctrl ↵: open in a new window. */
 openInNewWindow: isMac ? '⌘↵' : 'Ctrl ↵',
};
export const shellShortcuts = {
 rail: formatShortcut('⌘/Ctrl S'),
 focus: formatShortcut('⌘/Ctrl ⇧ F'),
 history: formatShortcut('⌘/Ctrl Y'),
 tasks: formatShortcut('⌘/Ctrl ⇧ K'),
 palette: formatShortcut('⌘/Ctrl K'),
 settings: formatShortcut('⌘/Ctrl ,'),
 /** Shell 3f draws the glyph against the letter (⌘O), so this one does not go through formatShortcut's spaced form. */
 openFile: isMac ? '⌘O' : 'Ctrl O',
 allModels: formatShortcut('⌘/Ctrl /'),
 /** The pinned-model chord for the 1-based slot (design Interactions: ⌥⌘1–3). */
 pinnedModel: (slot: number) => (isMac ? `⌥⌘${slot}` : `Ctrl Alt ${slot}`),
};

type KeyEvent = Pick<KeyboardEvent, 'key' | 'code' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'> & { target?: EventTarget | null };

/**
 * Every window-level shortcut of the shell, as ids. This is the ONE place that decides which chord means what,
 * so two surfaces can never both claim a key (design Interactions "Shortcuts"). `jump` carries the tab digit and
 * `model-pin` the pinned-model slot (⌥⌘1–3). ⌘⇧\ (Ctrl Shift A on Linux) is the tab overview; ⌘↑ and ⌘↓ step between
 * messages in a chat; Home uses ⌘↑ for `up-level`. I2.6 adds ⌘J for `next-up` and ⌘[ / ⌘] for focus history
 * (`back` / `forward`, Alt arrows off a Mac per P-5). ⌘/Ctrl B for the rail keeps working beside ⌘S. 'terminal-new' is ⌃` on every platform (the chord
 * features/terminal/keys.ts re-exports). 'open-file' (⌘/Ctrl O) belongs to the new-tab field: only that surface claims it,
 * and choosing it is the Open file… row. A prose field that already holds words keeps both chords.
 */
export type ShortcutId =
 | 'new' | 'close' | 'reopen' | 'group' | 'next' | 'previous' | 'switch' | 'switch-back' | 'jump'
 | 'open-file' | 'terminal-new' | 'copy-link' | 'quick-look' | 'overview' | 'turn-previous' | 'turn-next' | 'history' | 'tasks-panel' | 'rail' | 'focus' | 'palette' | 'models' | 'model-pin' | 'settings'
 /** Places (Interactions "Shortcuts"): ⌘P Go to a place, ⌘⇧P All places, ⌘0 this place's Home, ⌘⇧W close this place,
  * ⌘N a new window on Now, ⌘Z undo the last structural action. `place-jump` carries the rail slot: 0 is Now, 1–9 the
  * pinned-then-open places (⌃ on a Mac; Alt elsewhere, because Ctrl+digit is already the tab jump there). */
 | 'next-up' | 'back' | 'forward' | 'up-level'
 | 'goto' | 'all-places' | 'place-home' | 'place-jump' | 'close-place' | 'new-window' | 'undo'
 /** The web pane's chords (⌘L address, ⌘R reload, ⌘F find). A web page holds the keys, so the native menu sends the same ids. */
 | 'web-address' | 'web-reload' | 'web-find';
export type Shortcut = { id: ShortcutId; index?: number };

/** A text field with words in it keeps ⌘↑ and ⌘↓ as caret keys. */
function isWritingField(target: EventTarget | null | undefined): boolean {
 const field = target as { tagName?: string; value?: string } | null | undefined;
 return (field?.tagName === 'TEXTAREA' || field?.tagName === 'INPUT') && (field.value?.length ?? 0) > 0;
}

/** A field, editor or terminal keeps ⌘Z for its own text. */
function isEditable(target: EventTarget | null | undefined): boolean {
 const element = target as { tagName?: string; isContentEditable?: boolean; closest?: (selector: string) => unknown } | null | undefined;
 return element?.tagName === 'TEXTAREA' || element?.tagName === 'INPUT' || !!element?.isContentEditable || !!element?.closest?.('[contenteditable]:not([contenteditable="false"]), .xterm');
}

/** Space keeps native activation on buttons and editing controls, including their nested icon targets. */
function keepsSpace(target: EventTarget | null | undefined): boolean {
 const element = target as Element | null | undefined;
 return isEditable(target) || element?.tagName === 'BUTTON' || element?.tagName === 'SELECT' || !!element?.closest?.('button, [role="button"], select');
}

/** True when the event came from inside an xterm field (its hidden textarea), including one inside a portal or a second tab. */
export function isTerminalTarget(target: EventTarget | null | undefined): boolean {
 return typeof (target as Element | null | undefined)?.closest === 'function' && !!(target as Element).closest('.xterm');
}

/**
 * A prose field that already holds words keeps ⌃` and ⌘O, the same way it keeps ⌘↑ for the caret.
 * The new-tab command field is not prose: Shell 3f draws both hints beside a typed query.
 * A terminal's hidden textarea is not prose either: ⌃` opens another shell from inside one.
 */
function typingInProseField(target: EventTarget | null | undefined): boolean {
 if (isTerminalTarget(target)) return false;
 const field = target as { getAttribute?: (name: string) => string | null } | null | undefined;
 if (field?.getAttribute?.('role') === 'combobox') return false;
 return isWritingField(target);
}

/**
 * Off a Mac the primary modifier is Ctrl, which is also the shell's editing modifier (Ctrl+W delete word, Ctrl+K kill
 * line, Ctrl+S stop output, Ctrl+Y yank, Ctrl+1..9). Inside a terminal field those chords therefore belong to the
 * PTY. The desktop chords there are the ones the GNOME Terminal convention reserves: Ctrl+Shift+T / Ctrl+Shift+W
 * for a new / closed tab, plus other Ctrl+Shift chords except the shell's Copy, Ctrl+` and Ctrl+Tab. Mac Cmd chords never reach a shell.
 */
export type ShortcutContext = { mac?: boolean; terminal?: boolean; home?: boolean };

/** Home owns Up only outside its composer, so a field keeps its native caret movement even when empty. */
function isHomeTarget(target: EventTarget | null | undefined): boolean {
 const element = target as Element | null | undefined;
 return !!element?.closest?.('.home-pane, .home-page');
}
function terminalShortcutOf(event: KeyEvent): Shortcut | undefined {
 const bare = event.ctrlKey && !event.metaKey && !event.altKey;
 if (bare && event.shiftKey) {
  const key = event.key.toLowerCase();
  if (key === 't') return { id: 'new' };
  if (key === 'w') return { id: 'close' };
  if (key === 'c') return;
 }
 if (bare && (event.shiftKey || event.code === 'Backquote' || event.key === 'Tab' || event.key === 'PageDown' || event.key === 'PageUp')) return shortcutOf(event, false);
}

export function shortcutOf(event: KeyEvent, platform: boolean | ShortcutContext = isMac): Shortcut | undefined {
 const context: ShortcutContext = typeof platform === 'boolean' ? { mac: platform } : platform;
 const mac = context.mac ?? isMac;
 if (context.terminal && !mac) return terminalShortcutOf(event);
 if (event.key === ' ' && !context.terminal && !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey && !keepsSpace(event.target)) return { id: 'quick-look' };
 if (event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey && event.code === 'Backquote' && !typingInProseField(event.target)) return { id: 'terminal-new' };
 // The place slots: ⌃0–9 on a Mac, Alt 0–9 elsewhere. The digit comes from `code` so a layout's symbols do not matter.
 const slot = /^(?:Digit|Numpad)([0-9])$/.exec(event.code ?? '');
 if (slot && !event.shiftKey && (mac ? event.ctrlKey && !event.metaKey && !event.altKey : event.altKey && !event.ctrlKey && !event.metaKey)) return { id: 'place-jump', index: Number(slot[1]) };
 if (event.ctrlKey && !event.metaKey && !event.altKey && event.key === 'Tab') return { id: event.shiftKey ? 'switch-back' : 'switch' };
 const primaryKey = mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
 // ⌥⌘1–3 pick a pinned model. The digit comes from `code`: Option turns the key into a symbol on a Mac.
 if (!mac && event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey) {
  if (event.key === 'ArrowLeft') return { id: 'back' };
  if (event.key === 'ArrowRight') return { id: 'forward' };
 }
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
  // The conversation surface claims this chord before the workspace (Interactions I-ICV-34 and I-IKY-16).
  if (key === 'k') return { id: 'tasks-panel' };
  if (key === 'f') return { id: 'focus' };
  if (key === 'p') return { id: 'all-places' };
  // A terminal keeps this chord for copying its selection; other surfaces decide whether to copy a path or a link.
  if (key === 'c' && !context.terminal && !isTerminalTarget(event.target)) return { id: 'copy-link' };
  if (key === 'w') return { id: 'close-place' };
  return;
 }
 if (key === 'j') return { id: 'next-up' };
 if (mac && (event.code === 'BracketLeft' || key === '[')) return { id: 'back' };
 if (mac && (event.code === 'BracketRight' || key === ']')) return { id: 'forward' };
 if (key === 'p') return { id: 'goto' };
 if (key === '0') return { id: 'place-home' };
 if (key === 'n') return { id: 'new-window' };
 if (key === 'z' && !isEditable(event.target)) return { id: 'undo' };
 if (key === 't') return { id: 'new' };
 if (key === 'w') return { id: 'close' };
 if (key === 'g') return { id: 'group' };
 if (key === 'l') return { id: 'web-address' };
 if (key === 'r') return { id: 'web-reload' };
 if (key === 'f') return { id: 'web-find' };
 if (key === 'o' && !typingInProseField(event.target)) return { id: 'open-file' };
 if (key === 's' || key === 'b') return { id: 'rail' };
 if (key === 'y') return { id: 'history' };
 if (key === 'k') return { id: 'palette' };
 if (key === ',') return { id: 'settings' };
 if (key === '/') return { id: 'models' };
 if (/^[1-9]$/.test(key)) return { id: 'jump', index: Number(key) };
 const home = context.home ?? isHomeTarget(event.target);
 if (home) {
  if (event.key === 'ArrowUp' && !isEditable(event.target) && !(event.target as Element | null)?.closest?.('.composer-dock')) return { id: 'up-level' };
  return;
 }
 if (event.key === 'ArrowUp' && !isWritingField(event.target)) return { id: 'turn-previous' };
 if (event.key === 'ArrowDown' && !isWritingField(event.target)) return { id: 'turn-next' };
}

/** A handler returns true when it used the shortcut; the registry then stops and cancels the browser's own. */
export type ShortcutHandler = (shortcut: Shortcut, event: KeyboardEvent) => boolean | void;
/** Higher layers see a chord first: the surface on screen (composer, turns), then the workspace, then the app. */
export const shortcutLayer = { surface: 30, workspace: 20, app: 10 } as const;

const handlers: { layer: number; handler: ShortcutHandler }[] = [];
function dispatchShortcut(event: KeyboardEvent) {
 const shortcut = shortcutOf(event, { terminal: isTerminalTarget(event.target) });
 if (!shortcut) return;
 for (const { handler } of [...handlers]) {
  if (handler(shortcut, event)) { event.preventDefault(); event.stopPropagation(); return; }
 }
}
/** Runs a shortcut a native menu item chose through the same handlers a keypress reaches; true when one used it. */
export function runShortcut(shortcut: Shortcut): boolean {
 const event = new KeyboardEvent('keydown', { cancelable: true });
 return [...handlers].some(({ handler }) => handler(shortcut, event));
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

/**
 * Copy link (Shell 3g): the same chord as Copy path. A file or diff tab keeps it as Copy path (its own header binds it),
 * and a terminal field keeps it as the shell's copy; everywhere else it copies the active tab's link.
 */
export const copyLinkShortcut = copyPathShortcut;
export const isCopyLinkShortcut = isCopyPathShortcut;

/** Interactive shell tabs: Control-backtick is identical on every platform. */
export const newTerminalShortcut = isMac ? '⌃`' : 'Ctrl `';
export const isNewTerminalShortcut = (event: KeyEvent) => shortcutOf(event)?.id === 'terminal-new';
/** In a terminal field off a Mac: Ctrl+Shift+T and Ctrl+Shift+W are the new and close tab chords. */
export const terminalTabShortcuts = { new: isMac ? '⌘ T' : 'Ctrl Shift T', close: isMac ? '⌘ W' : 'Ctrl Shift W' };

/** The new-tab field: ⌘↵ (Ctrl ↵) opens every History match for the words typed ("See all N in History"). */
export const seeAllHistoryShortcut = isMac ? '⌘↵' : 'Ctrl ↵';
export function isSeeAllHistoryShortcut(event: KeyEvent) {
 const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
 return primary && !event.shiftKey && !event.altKey && event.key === 'Enter';
}

/**
 * Spells a chord the way the design's Kbd does (C-CTRL-14, C-MENU-5): glyphs run together on a Mac (⌘⇧C), words join
 * with "+" elsewhere (Ctrl+Shift+C). The platform is read at call time; the user agent decides only when the platform is empty, because
 * WebKit's user agent says Mac on every OS and would otherwise run the words of a non-Mac chord together ("AltEnter").
 */
export function spellShortcut(label: string) {
 const mac = navigator.platform ? /Mac/.test(navigator.platform) : /Mac/.test(navigator.userAgent);
 const parts = label.replace('⌘/Ctrl', mac ? '⌘' : 'Ctrl').replace('⇧', mac ? '⇧' : 'Shift').split(/\s+/).filter(Boolean);
 // A label already spelled in words (Ctrl, Shift) stays in words even when only the user agent says Mac, as WebKit does on Linux.
 const words = parts.some(part => /^(Ctrl|Shift|Alt|Meta)$/.test(part));
 return mac && !words ? parts.join('') : parts.join('+');
}
