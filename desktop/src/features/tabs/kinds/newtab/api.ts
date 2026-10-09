import type { WorkspaceController } from '../../../workspace-sync/controller';
import { createContext, useContext, type Dispatch } from 'react';
import type { TabSummary } from '../../../conversation/tabSummary';
import type { WorkspaceAction, WorkspaceState } from '../../model';

/** What the new-tab field may read of the workspace and do to it. Workspace provides it once; the field never reaches for globals. */
export type NewTabHost = {
  state: WorkspaceState;
  summaries: Readonly<Record<string, TabSummary>>;
  dispatch: Dispatch<WorkspaceAction>;
  /** Closes with focus restoration. */
  closeTab: (id: string) => void;
  receiveTransfer?: WorkspaceController['receiveTransfer'];
};

export const NewTabHostContext = createContext<NewTabHost | null>(null);
export const useNewTabHost = (): NewTabHost | null => useContext(NewTabHostContext);
