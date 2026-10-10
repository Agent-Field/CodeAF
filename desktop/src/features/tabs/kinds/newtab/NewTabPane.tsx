import { startFor, startSentence } from '../../../terminal/open';
import { bind } from '../../../terminal/bindings';
import { useContext, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { TextInput } from '../../../../components/ui';
import { isMac, shellShortcuts, shortcutLayer, isSeeAllHistoryShortcut, seeAllHistoryShortcut } from '../../../../design/keyboard';
import { useHistoryHost } from '../../../history/host';
import { useShortcuts } from '../../../../design/useShortcuts';
import { connectEngine, sendEngine } from '../../../chat/engine-client';
import { FirstTurnContext, NewConversationPlaceContext } from '../../../conversation/firstTurn';
import { panesOf, tabHolding, visibleTabs } from '../../model';
import { newTabFieldEvent } from '../../../shell/newTabField';
import { historyKind } from '../history';
import { terminalKind, newTerminalShortcut } from '../terminal';
import { webKind } from '../web';
import type { PaneRenderProps } from '../slots';
import { useNewTabHost, type NewTabHost } from './api';
import { siteOf } from '../../../web/address';
import { buildSections, flatRows, tabDigit, titleFromText, type NewTabRow } from './rows';
import { filePromptArmed, settleFilePrompt } from './openFile';
import { useFileMatches } from './useFileMatches';
import { useHistoryMatches } from './useHistoryMatches';
import { NewTabView } from './NewTabView';

const shortcut = (digit?: number) => (digit === undefined ? undefined : isMac ? `⌘${digit}` : `Ctrl ${digit}`);
const terminalShortcut = newTerminalShortcut;
const fileShortcut = shellShortcuts.openFile;
const caption = 'Type a question, a file, a URL, or a command.';
const fileCaption = 'Type part of a file name.';

/**
 * The new tab (design 3f): one field that starts a conversation, opens a file or jumps to a tab. It never makes an
 * engine call until a row is chosen. An address opens as a web tab (the first row); the conversation row below it still asks the same words.
 */
export function NewTabPane({ pane, focused, actions }: PaneRenderProps) {
  const host = useNewTabHost();
  if (!host) return null;
  return <NewTabField host={host} paneId={pane.id} focused={focused} draft={pane.draft} onDraft={actions.onDraft}/>;
}

function NewTabField({ host, paneId, focused, draft, onDraft }: { host: NewTabHost; paneId: string; focused: boolean; draft: string; onDraft: (draft: string) => void }) {
  const { state, summaries, dispatch, closeTab } = host;
  // What is typed lives in the pane's draft too, so leaving the tab and coming back finds the words and the rows still there.
  const [query, setQueryState] = useState(draft);
  const setQuery = (text: string) => { setQueryState(text); onDraft(text); };
  const history = useHistoryHost();
  const [index, setIndex] = useState(0);
  // ⌘O from another tab arms this before the field mounts. Reading it here shows the file caption on the first paint.
  const [filing, setFiling] = useState(() => filePromptArmed(paneId));
  const [busy, setBusy] = useState(false);
  const beforeFirstTurn = useContext(FirstTurnContext);
  const newConversationPlace = useContext(NewConversationPlaceContext);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => { if (focused) input.current?.focus(); }, [focused]);
  // ⌘K while this empty tab is already the one showing has no tab change to focus from, so it asks directly.
  useEffect(() => {
    const onField = () => { if (focused) input.current?.focus(); };
    window.addEventListener(newTabFieldEvent, onField);
    return () => window.removeEventListener(newTabFieldEvent, onField);
  }, [focused]);
  // A New tab that was already open is still mounted, so the arm is taken when it becomes the pane in front.
  // The settle waits a tick: StrictMode replays the effect, and clearing in the first pass would drop the prompt.
  useEffect(() => {
    if (!filePromptArmed(paneId)) return;
    setFiling(true);
    input.current?.focus();
    const timer = window.setTimeout(() => settleFilePrompt(paneId), 0);
    return () => window.clearTimeout(timer);
  }, [paneId, focused]);

  const others = useMemo(() => {
    const all = visibleTabs(state);
    const self = tabHolding(state, paneId)?.id;
    return all.flatMap((tab, position) => (tab.id === self || tab.kind === 'newtab' ? [] : [{ tab, shortcut: shortcut(tabDigit(position, all.length)), waiting: summaries[tab.id]?.mark === 'waiting' }]));
  }, [state, summaries, paneId]);
  // The field has no session; it searches through the first saved conversation (a file tab reads through the same one).
  const sessionFile = state.tabs.flatMap(tab => (tab.split ? tab.split.panes : [tab])).find(pane => pane.sessionFile && pane.kind === 'conversation')?.sessionFile;
  const files = useFileMatches(query, sessionFile);
  // History answers only when it is backed and the workspace gave the field its host; otherwise the section is absent, not broken.
  const asking = historyKind.backed && !!history;
  const found = useHistoryMatches(query, asking);
  const sections = useMemo(() => buildSections({ query, tabs: others, closed: state.closed, files, terminal: terminalKind.backed, web: webKind.backed, terminalShortcut, fileShortcut, history: found, seeAllShortcut: seeAllHistoryShortcut }), [query, others, state.closed, files, found]);
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
      const snapshot = await connectEngine(undefined, newConversationPlace);
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
  function openConversation(row: NewTabRow, press: { background: boolean }) {
    const item = row.conversation;
    if (!history || !item) return;
    const open = state.tabs.flatMap(panesOf).some(one => one.sessionFile === item.sessionFile);
    history.continueConversation(item, paneId, press.background ? { newTab: true, background: true } : { newTab: false });
    // An open conversation is only selected, so the empty field has nothing left to do (as with an open-tab row).
    if (open && !press.background) closeSelf();
  }
  /** Every match in History's own search: the field's tab becomes History, or the History tab that exists is shown with the words. */
  function seeAll() {
    const words = query.trim();
    if (!words || !asking) return;
    const existing = state.tabs.flatMap(panesOf).find(one => one.kind === 'history');
    if (existing) { dispatch({ type: 'draft', id: existing.id, draft: words }); dispatch({ type: 'select', id: existing.id }); closeSelf(); }
    else dispatch({ type: 'newtab-become', id: paneId, kind: 'history', title: historyKind.label, titleSource: 'engine', draft: words });
  }
  function pick(row: NewTabRow | undefined, press: { background: boolean } = { background: false }) {
    if (!row || busy) return;
    if (row.kind === 'history') openConversation(row, press);
    else if (row.kind === 'seeall') seeAll();
    else if (row.kind === 'ask') void ask(query.trim());
    else if (row.kind === 'web') dispatch({ type: 'newtab-become', id: paneId, kind: 'web', title: siteOf(row.target!), titleSource: 'message', target: { url: row.target! } });
    else if (row.kind === 'terminal') void openShell();
    else if (row.kind === 'openfile') { setFiling(true); input.current?.focus(); }
    else if (row.kind === 'file') dispatch({ type: 'newtab-become', id: paneId, kind: 'file', title: row.label, path: row.target, sessionFile });
    else if (row.kind === 'tab') { dispatch({ type: 'select', id: row.target! }); closeSelf(); }
    else dispatch({ type: 'newtab-reopen', id: paneId, closedId: row.target! });
  }
  // ⌘/Ctrl O while this field is in front is the Open file… row's own pick. From any other tab the workspace hook opens or focuses a New tab and arms the same caption.
  useShortcuts(shortcutLayer.surface, shortcut => {
    if (shortcut.id !== 'open-file' || document.querySelector('dialog[open]')) return false;
    pick(rows.find(row => row.kind === 'openfile'));
    return true;
  }, focused);
  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.nativeEvent.isComposing) return;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const step = event.key === 'ArrowDown' ? 1 : -1;
      setIndex(current => (Math.min(current, rows.length - 1) + step + rows.length) % rows.length);
    } else if (isSeeAllHistoryShortcut(event)) {
      event.preventDefault();
      seeAll();
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
