import type { ComponentType } from 'react';
import type { IconName } from '../../../components/ui';
import type { TabSummary } from '../../conversation/tabSummary';
import type { Pane } from '../types';
import type { TabView } from '../view-state';
import type { TabKind } from './types';

/** What a pane may do to its own state. The workspace routes each call to the right pane by id. */
export type PaneActions = {
  onDraft: (draft: string) => void;
  onView: (change: TabView) => void;
  onSummary: (summary: TabSummary) => void;
  /** Opens a task from this pane as a background task tab. */
  onOpenTaskTab: (taskId: string, title: string) => void;
  /** Opens a workspace file from this pane as a file tab, or its changes as a diff tab. */
  onOpenFile: (path: string, kind: 'file' | 'diff') => void;
};

/** The pane renderer slot: the body a kind draws inside the card (or inside one pane of a split). */
export type PaneRenderProps = { pane: Pane; label: string; focused: boolean; split: boolean; actions: PaneActions };
/** The preview renderer slot: the text body of a hover preview or an overview card for this kind. */
export type PreviewRenderProps = { pane: Pane; summary?: TabSummary };

export type KindDef = {
  kind: TabKind;
  /** Person-facing kind name, shown in previews and menus. */
  label: string;
  /** Registry icon name. A web tab draws a favicon or monogram instead (see KindIcon). */
  icon: IconName;
  /** True when the engine or bridge backs this kind today. Unbacked kinds are built and specimened, never opened in the live app. */
  backed: boolean;
  pane: ComponentType<PaneRenderProps>;
  /** Null until the hover-preview lane fills it; consumers then fall back to the title and draft. */
  preview: ComponentType<PreviewRenderProps> | null;
};
