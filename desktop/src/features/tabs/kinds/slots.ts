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
};

/** The pane renderer slot: the body a kind draws inside the card (or inside one pane of a split). */
export type PaneRenderProps = { pane: Pane; label: string; focused: boolean; split: boolean; actions: PaneActions };
/** What a preview card may do for the person: answer what is asked, or open the tab. Built by the preview host. */
export type PreviewActions = {
  /** True while an answer is on its way, so the buttons wait. */
  busy: boolean;
  /** Why the last answer did not go through, in words; cleared by the next attempt. The card keeps asking. */
  error?: string;
  /** Answers every permission the tab is asking, by role. */
  allowAll: () => void;
  /** Opens the tab, where the full question is. */
  review: () => void;
};
/** The preview renderer slot: the text card a hover preview or an overview card draws for this kind (Shell 3k). */
export type PreviewRenderProps = { pane: Pane; title: string; summary?: TabSummary; now: number; act: PreviewActions };

export type KindDef = {
  kind: TabKind;
  /** Person-facing kind name, shown in previews and menus. */
  label: string;
  /** Registry icon name. A web tab draws a favicon or monogram instead (see KindIcon). */
  icon: IconName;
  /** True when the engine or bridge backs this kind today. Unbacked kinds are built and specimened, never opened in the live app. */
  backed: boolean;
  pane: ComponentType<PaneRenderProps>;
  /** The text card for this kind: kind, state, title and the one piece that matters. */
  preview: ComponentType<PreviewRenderProps>;
};
