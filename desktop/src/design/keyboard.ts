/** Platform conventions shared by controls, menus and workspace hints. */
export const isMac = /Mac/.test(navigator.platform);
export function formatShortcut(label: string) {
 return label.replace('⌘/Ctrl', isMac ? '⌘' : 'Ctrl').replace('⇧', isMac ? '⇧' : 'Shift');
}
export const tabShortcuts = {
 new: formatShortcut('⌘/Ctrl T'),
 close: formatShortcut('⌘/Ctrl W'),
 reopen: formatShortcut('⌘/Ctrl ⇧ T'),
 switch: isMac ? '⌃ Tab' : 'Ctrl Tab',
 switchBack: isMac ? '⌃ ⇧ Tab' : 'Ctrl Shift Tab',
};
export const overviewShortcut = isMac ? '⌘ ⇧ \\' : 'Ctrl Shift A';
type KeyEvent = Pick<KeyboardEvent, 'key' | 'code' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'>;
export function isOverviewShortcut(event: KeyEvent) {
 return !event.altKey && event.shiftKey && (isMac
  ? event.metaKey && !event.ctrlKey && event.code === 'Backslash'
  : event.ctrlKey && !event.metaKey && event.key.toLowerCase() === 'a');
}
export function sequentialTabDirection(event: KeyEvent): number {
 if (event.altKey) return 0;
 if (isMac && event.metaKey && event.shiftKey && !event.ctrlKey) {
  return event.code === 'BracketRight' ? 1 : event.code === 'BracketLeft' ? -1 : 0;
 }
 if (!isMac && event.ctrlKey && !event.metaKey && !event.shiftKey) {
  return event.key === 'PageDown' ? 1 : event.key === 'PageUp' ? -1 : 0;
 }
 return 0;
}
export function tabActionShortcut(event: KeyEvent): 'new' | 'reopen' | 'close' | undefined {
 const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
 if (!primary || event.altKey) return;
 const key = event.key.toLowerCase();
 if (key === 't') return event.shiftKey ? 'reopen' : 'new';
 if (key === 'w' && !event.shiftKey) return 'close';
}

/** Final Tab States: primary Enter starts a section; plain Enter continues/steers. */
export const composerShortcuts = { send: 'Enter', steer: 'Enter', queue: isMac ? '⌃ Enter' : 'Queue menu', newSection: formatShortcut('⌘/Ctrl Enter') };
export function composerActionShortcut(event: KeyEvent, running: boolean): 'send' | 'steer' | 'queue' | 'restart' | undefined {
 if (event.key !== 'Enter' || event.altKey || event.shiftKey) return;
 const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
 if (primary) return 'restart';
 if (isMac && running && event.ctrlKey && !event.metaKey) return 'queue';
 if (!event.metaKey && !event.ctrlKey) return running ? 'steer' : 'send';
}
export function isWorkShortcut(event: KeyEvent): 'focus'|'preset'|'fold'|undefined {
 const primary=isMac?event.metaKey&&!event.ctrlKey:event.ctrlKey&&!event.metaKey;
 if(!primary||event.altKey)return;
 if(event.shiftKey&&event.key.toLowerCase()==='f')return 'fold';
 if(event.shiftKey)return;
 if(event.key.toLowerCase()==='l')return 'focus';
 if(event.key==='.')return 'preset';
}
