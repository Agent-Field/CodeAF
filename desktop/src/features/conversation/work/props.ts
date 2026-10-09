import type { ReactNode } from 'react';

/** What a file chip may wear next to the name (edit: +N −M, write: +N lines). */
export type FileStat = { added?: number; removed?: number; capped?: boolean };

export type ReadFull = (callId: string) => Promise<{ output: string; full: boolean }>;

/** Render props the assets lane fills; every default is plain text. */
export type WorkRender = {
  renderFile?: (path: string, stat?: FileStat) => ReactNode;
  renderLink?: (url: string) => ReactNode;
  readFull?: ReadFull;
};

/** A call row's state: the contract's four, plus the three a live step can be in before or beside them. */
export type RowState = 'forming' | 'waiting' | 'running' | 'done' | 'failed' | 'stopped' | 'refused';
