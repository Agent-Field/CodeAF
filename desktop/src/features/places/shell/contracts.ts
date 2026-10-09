// The contracts between the Places shell's parts: the controller (App), the rail, the Go to chooser, the Home pane,
// the undo toast and the place dialogs. Each part is built against these shapes and nothing else, so a part can be
// drawn in the Design system specimen with plain data and wired in the window with the live graph.
//
// Two different things are called "places" in this program and they never mix here: a conversation's SOURCE FOLDERS
// (filesystem paths the engine reports on a world row) and the DESIGN-GRAPH PLACES (named, tinted, many-to-many
// containers kept by internal/placegraph). Everything in this file is the second kind.

import type { Tint } from '../client';
export type { ToastAction, ToastModel } from '../../../components/ui/Toast';

/** What one window shows: Now (the unplaced tabs) or one place of the graph. All places is a tab, never a window place. */
export type WindowPlace = 'now' | `pl_${string}`;

/** A place as the rail and the chooser draw it. Built from a `PlaceView` by `railPlace()` (shell/selectors.ts). */
export type PlaceRowModel = {
  id: string;
  name: string;
  tint: Tint;
  /** The first parent's name, drawn muted after the name ("Config parser · codeaf"). Absent at the top level. */
  parentName?: string;
  /** Every parent's id, for the chooser's `parent` mode: a place's current parents are not offered as a second parent. */
  parents?: readonly string[];
  /** Amber when something needs the person, red when a task failed. Running never draws a dot (Places 10a). */
  status?: 'waiting' | 'failed';
  /** The dot's words, for its tooltip and for a screen reader ("2 need you in Config parser"). */
  statusLabel?: string;
  /** Closed in this window but still running or waiting: the row stays in Open, muted, until the work settles. */
  closedButBusy?: boolean;
  pinned: boolean;
  archived: boolean;
  /** ISO instant the person last went to it, for the chooser's Recent section. */
  lastOpenedAt?: string;
  /** "4 inside" when it has children, otherwise "12 chats"; absent when both are zero (the emptiness law). */
  meta?: string;
};

/** Which question the Go to chooser is answering. Every mode but `go` ends in one write; the chooser itself writes nothing. */
export type ChooserMode =
  | { kind: 'go' }
  /** Merge `placeId` into the chosen place. The place itself and its descendants are not offered. */
  | { kind: 'merge'; placeId: string; placeName: string }
  /** Add `placeId` under a second parent. Itself, its descendants and its current parents are not offered. */
  | { kind: 'parent'; placeId: string; placeName: string }
  /** File chats in the chosen place. The places they are already in are not offered. */
  | { kind: 'file'; chatIds: readonly string[]; chatTitle: string; exclude: readonly string[] };

export type ChooserProps = {
  open: boolean;
  mode: ChooserMode;
  /** Every non-archived place in the graph. The chooser filters and orders; it never fetches. */
  places: readonly PlaceRowModel[];
  /** Children by parent id, for the tree under "All" (top level under `root`). Order is the graph's. */
  childrenOf: ReadonlyMap<string, readonly string[]>;
  /** Total non-archived places, for the "58 places" count. */
  total: number;
  /** Now, for relative times in Recent; injected so tests do not depend on the clock. */
  now: Date;
  /** ↵ (or a click) on a row. In `go` mode the owner goes there; in the others it performs the write. A rejection is shown in the chooser. */
  onChoose: (placeId: string) => void | Promise<void>;
  /** ⌘↵ / Ctrl↵ in `go` mode: open the place in a new window. Absent where there are no windows to open: the hint is then not drawn. */
  onChooseInNewWindow?: (placeId: string) => void | Promise<void>;
  /** ⌘N / Ctrl N with a typed name: create a place with that name (and, outside `go` mode, then use it). Absent = no create. */
  onCreate?: (name: string) => void | Promise<void>;
  onClose: () => void;
};

/** One undoable structural action, kept as the engine's receipts (Interactions: "⌘Z undoes the last structural action, up to 20 steps"). */
export type UndoEntry = {
  id: string;
  /** The toast's sentence, in the person's words: "Moved “Q3 report” into Reports". */
  text: string;
  /** The place the sentence names in medium weight, when there is one. */
  subject?: string;
  receipts: readonly string[];
};
