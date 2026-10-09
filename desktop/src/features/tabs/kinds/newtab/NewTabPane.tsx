import { startFor, startSentence } from '../../../terminal/open';
import { bind } from '../../../terminal/bindings';
import { useContext, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { TextInput } from '../../../../components/ui';
import { isMac } from '../../../../design/keyboard';
import { connectEngine, sendEngine } from '../../../chat/engine-client';
import { FirstTurnContext } from '../../../conversation/firstTurn';
import { tabHolding, visibleTabs } from '../../model';
import { terminalKind, newTerminalShortcut } from '../terminal';
import type { PaneRenderProps } from '../slots';
import { useNewTabHost, type NewTabHost } from './api';
import { siteOf } from '../../../web/address';
import { buildSections, flatRows, tabDigit, titleFromText, type NewTabRow } from './rows';
import { useFileMatches } from './useFileMatches';
import { NewTabView } from './NewTabView';

const shortcut = (digit?: number) => (digit === undefined ? undefined : isMac ? `⌘${digit}` : `Ctrl ${digit}`);
const terminalShortcut = newTerminalShortcut;
const fileShortcut = isMac ? '⌘O' : 'Ctrl O';
const caption = 'Type a question, a file, a URL, or a command.';
const fileCaption = 'Type part of a file name.';

/**
 * The new tab (design 3f): one field that starts a conversation, opens a file or jumps to a tab. It never makes an
 * engine call until a row is chosen. An address opens as a web tab (the first row); the conversation row below it still asks the same words.
 */
export function NewTabPane({ pane, focused }: PaneRenderProps) {
  const host = useNewTabHost();
  if (!host) return null;
  return <NewTabField host={host} paneId={pane.id} focused={focused}/>;
}

function NewTabField({ host, paneId, focused }: { host: NewTabHost; paneId: string; focused: boolean }) {
  const { state, summaries, dispatch, closeTab } = host;
  const [query, setQuery] = useState('');
  const [index, setIndex] = useState(0);
  const [filing, setFiling] = useState(false);
  const [busy, setBusy] = useState(false);
  const beforeFirstTurn = useContext(FirstTurnContext);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => { if (focused) input.current?.focus(); }, [focused]);

  const others = useMemo(() => {
    const all = visibleTabs(state);
    const self = tabHolding(state, paneId)?.id;
    return all.flatMap((tab, position) => (tab.id === self || tab.kind === 'newtab' ? [] : [{ tab, shortcut: shortcut(tabDigit(position, all.length)), waiting: summaries[tab.id]?.mark === 'waiting' }]));
  }, [state, summaries, paneId]);
  // The field has no session; it searches through the first saved conversation (a file tab reads through the same one).
  const sessionFile = state.tabs.flatMap(tab => (tab.split ? tab.split.panes : [tab])).find(pane => pane.sessionFile && pane.kind === 'conversation')?.sessionFile;
  const files = useFileMatches(query, sessionFile);
  const sections = useMemo(() => buildSections({ query, tabs: others, closed: state.closed, files, terminal: terminalKind.backed, terminalShortcut, fileShortcut }), [query, others, state.closed, files]);
  const rows = flatRows(sections);
  const active = rows[Math.min(index, rows.length - 1)];

  function closeSelf() {
    const holder = tabHolding(state, paneId);
    if (holder?.split) dispatch({ type: 'split-close-pane', id: holder.id, paneId });
    else closeTab(paneId);
  }
  async function ask(text: string) {
    setBusy(true);
    let sessionFile: string | undefined;
    let draft = text;
    try {
      const snapshot = await connectEngine();
      sessionFile = snapshot.sessionFile;
      // A question asked from a place's new tab is filed in that place before its first turn, exactly as the
      // conversation's own first send is; if filing is refused the conversation opens holding the words, unsent.
      if (beforeFirstTurn && sessionFile) await beforeFirstTurn(sessionFile);
      await sendEngine(snapshot.id, text);
      draft = '';
    } catch { /* The conversation opens with the words kept as its draft; sending again is one key. */ }
    dispatch({ type: 'newtab-become', id: paneId, kind: 'conversation', title: titleFromText(text), titleSource: 'message', sessionFile, draft });
  }
  async function openShell() {
    setBusy(true);
    try {
      const { title, target } = await startFor(paneId, { sessionFile });
      dispatch({ type: 'newtab-become', id: paneId, kind: 'terminal', title, sessionFile: target.sessionFile, terminalId: target.terminalId });
    } catch (error) {
      bind(paneId, { sessionFile, refused: startSentence(error) });
      dispatch({ type: 'newtab-become', id: paneId, kind: 'terminal', title: 'Terminal', sessionFile });
    }
  }
  function pick(row: NewTabRow | undefined) {
    if (!row || busy) return;
    if (row.kind === 'ask') void ask(query.trim());
    else if (row.kind === 'web') dispatch({ type: 'newtab-become', id: paneId, kind: 'web', title: siteOf(row.target!), titleSource: 'message', target: { url: row.target! } });
    else if (row.kind === 'terminal') void openShell();
    else if (row.kind === 'openfile') { setFiling(true); input.current?.focus(); }
    else if (row.kind === 'file') dispatch({ type: 'newtab-become', id: paneId, kind: 'file', title: row.label, path: row.target, sessionFile });
    else if (row.kind === 'tab') { dispatch({ type: 'select', id: row.target! }); closeSelf(); }
    else dispatch({ type: 'newtab-reopen', id: paneId, closedId: row.target! });
  }
  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.nativeEvent.isComposing) return;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const step = event.key === 'ArrowDown' ? 1 : -1;
      setIndex(current => (Math.min(current, rows.length - 1) + step + rows.length) % rows.length);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      pick(active);
    } else if (event.key === 'Escape') {
      event.preventDefault();
      if (query) { setQuery(''); setIndex(0); } else closeSelf();
    }
  }

  const field = <TextInput ref={input} role="combobox" aria-label="Search or start" aria-expanded="true" aria-controls={`newtab-${paneId}`} aria-activedescendant={active ? `newtab-${paneId}-${active.id}` : undefined} aria-autocomplete="list" autoComplete="off" spellCheck={false} value={query} disabled={busy}
    onChange={event => { setQuery(event.target.value); setIndex(0); }} onKeyDown={onKeyDown}/>;
  return <NewTabView id={paneId} field={field} query={query} sections={sections} activeRowId={active?.id} caption={filing ? fileCaption : caption} enterHint={active?.kind === 'web' ? '↵ to open the page' : undefined}
    onHover={row => { const at = rows.indexOf(row); if (at !== index) setIndex(at); }} onPick={pick}/>;
}
