// The kinds registry: one entry per tab kind, looked up by kind. Consumers call kindDef(kind);
// there is no switch on kind anywhere. A lane fills its kind by editing ONLY its own file here.
import { conversationKind } from './conversation';
import { diffKind } from './diff';
import { fileKind } from './file';
import { historyKind } from './history';
import { homeKind } from './home';
import { inboxKind } from './inbox';
import { newtabKind } from './newtab';
import { settingsKind } from './settings';
import { taskKind } from './task';
import { terminalKind } from './terminal';
import { webKind } from './web';
import type { KindDef } from './slots';
import { kindOrDefault, tabKinds, type TabKind } from './types';

export const kindRegistry: Readonly<Record<TabKind, KindDef>> = {
  conversation: conversationKind, task: taskKind, file: fileKind, diff: diffKind, web: webKind,
  terminal: terminalKind, settings: settingsKind, history: historyKind, newtab: newtabKind, inbox: inboxKind, home: homeKind,
};

export const kindDef = (kind: TabKind): KindDef => kindRegistry[kindOrDefault(kind)];
export const allKinds = (): KindDef[] => tabKinds.map(kind => kindRegistry[kind]);
