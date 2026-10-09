import { createContext, useContext } from 'react';
import type { FileTabKind } from './fileTarget';

/** Opens a workspace file in a tab: a file tab for the whole file, a diff tab for what changed. */
export type OpenFile = (path: string, kind: FileTabKind) => void;

const OpenFileContext = createContext<OpenFile | undefined>(undefined);

/** Supplied around each pane by the workspace, so a chip anywhere in a conversation can open a tab from its own session. */
export const OpenFileProvider = OpenFileContext.Provider;

/** The opener for the pane this component is in, or undefined where no workspace is around (specimens). */
export const useOpenFile = (): OpenFile | undefined => useContext(OpenFileContext);
