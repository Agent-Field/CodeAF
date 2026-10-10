// Test-only entry: the council-aware conversation pane over a scripted fake council stream. No engine is involved.
import '../../../src/design/inputModality';
import '../../../src/App.css';
import { useState } from 'react';
import ReactDOM from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { CouncilClientContext } from '../../../src/features/council/useCouncilChat';
import type { Council, CouncilClient, CouncilMessage } from '../../../src/features/council/client';
import { kindDef } from '../../../src/features/tabs/kinds/registry';
import type { Pane } from '../../../src/features/tabs/types';

const base: Council = { id: 'cn1', places: ['marketing', 'software'], topic: 'Can the post promise trailing commas?', chatId: 'chat1', sessionFile: '/home/.codeaf/council-sessions/chat1/transcript.jsonl', label: 'Marketing with Software', turns: 2, cap: 6, spend: 0.01, capUSD: 0.25, state: 'running', openedAt: '2026-10-10T12:00:00Z' };
const lines: CouncilMessage[] = [
  { speaker: 'Marketing', text: 'Does the post get to promise trailing commas?' },
  { speaker: 'Software', text: 'Only outside strict mode.' },
];
const calls: string[] = [];
let council = base;
const w = window as unknown as { __council: unknown };
w.__council = {
  calls,
  say: (speaker: string, text: string) => lines.push({ speaker, text }),
  decide: () => { council = { ...council, state: 'decided', outcome: 'promise it outside strict mode', turns: 3, closedAt: '2026-10-10T12:05:00Z' }; },
  escalate: () => { council = { ...council, state: 'escalated', turns: 6, closedAt: '2026-10-10T12:06:00Z' }; },
  setSession: (file: string) => { listed = file; },
};
let listed = base.sessionFile as string;

const fake: CouncilClient = {
  list: async () => ({ councils: listed === base.sessionFile ? [council] : [] }),
  messages: async () => [...lines],
  pause: async () => { calls.push('pause'); council = { ...council, state: 'paused' }; return council; },
  resume: async () => { calls.push('resume'); council = { ...council, state: 'running' }; return council; },
  steer: async (_id, text) => { calls.push(`steer:${text}`); lines.push({ speaker: 'person', text }); council = { ...council, state: 'running' }; return council; },
};

const Pane = kindDef('conversation').pane;
function Harness() {
  const file = new URLSearchParams(location.search).get('session') ?? base.sessionFile;
  const [draft, setDraft] = useState('');
  const pane: Pane = { id: 'p1', kind: 'conversation', title: 'Marketing with Software', draft, sessionFile: file };
  const actions = { onDraft: setDraft, onView: () => undefined, onSummary: () => undefined, onOpenTaskTab: () => undefined, onRename: () => undefined, onOpenConversationTab: () => undefined, onOpenFile: () => undefined };
  return <div data-testid="pane" style={{ height: 700, display: 'flex' }}><Pane pane={pane} label={pane.title} focused split={false} actions={actions}/></div>;
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <ThemeProvider><CouncilClientContext.Provider value={fake}><Harness/></CouncilClientContext.Provider></ThemeProvider>,
);
