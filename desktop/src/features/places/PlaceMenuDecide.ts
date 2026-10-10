import type { MenuEntry } from '../../components/ui';
import { createDecisionsClient } from '../decisions/client.ts';
import { validatePlacesMutation } from './client.ts';
import type { PlacesShell } from './shell/PlacesShell';
import type { Mutation, PlaceDecide } from './wire';

/** What a menu choice changes. An absent field keeps the effective figure, so toggling Always ask never resets the threshold. */
export type DecideChange = { alwaysAsk?: boolean; threshold?: number };

/** The design allows 50 to 100 and starts at 90 (Iteration 2, P-11). The engine accepts 1 to 100; the menu offers the useful range. */
export const decideThresholds = [50, 60, 70, 80, 90, 95, 100] as const;

/** Only reached when the menu drew an entry, which needs the engine's own figures; the default keeps the type honest. */
const defaultDecide: PlaceDecide = { alwaysAsk: false, threshold: 90 };
const decisions = createDecisionsClient();

/** PUT /places/{id}/decide. The answer is a normal mutation, so the shell's one write path turns it into a toast with Undo. */
export async function putDecide(placeId: string, change: DecideChange, current: PlaceDecide, client: Pick<typeof decisions, 'setDecide'> = decisions): Promise<Mutation> {
  const body = { alwaysAsk: change.alwaysAsk ?? current.alwaysAsk, ...(change.threshold === undefined ? {} : { threshold: change.threshold }) };
  return validatePlacesMutation(await client.setDecide(placeId, body));
}

/** The sentence of the toast: what is now true, in the person's words. */
export function decideText(name: string, change: DecideChange): string {
  if (change.threshold !== undefined) return `Decision confidence for “${name}” is now ${change.threshold}%`;
  return change.alwaysAsk ? `“${name}” will always ask you` : `“${name}” decides automatically again`;
}

/** The one write both menus share: a receipt with Undo through the shell's ring, so a toggle is as undoable as a rename. */
export function writeDecide(shell: Pick<PlacesShell, 'write'>, placeId: string, name: string, current: PlaceDecide | undefined, change: DecideChange): Promise<unknown> {
  return shell.write(decideText(name, change), () => putDecide(placeId, change, current ?? defaultDecide), { subject: name });
}

/** The menu's two decision entries. They are absent when the engine did not say how the place decides (the emptiness law) or no handler is wired. */
export function decideEntries(placeId: string, decide: PlaceDecide | undefined, setDecide: ((placeId: string, change: DecideChange) => void | Promise<void>) | undefined): MenuEntry[] {
  if (!decide || !setDecide) return [];
  // A figure the engine holds outside the presets (an older write) is still shown, checked, rather than hidden.
  const figures = [...new Set([...decideThresholds, decide.threshold])].sort((a, b) => a - b);
  return [
    { id: 'always-ask', label: 'Always ask me', checked: decide.alwaysAsk, onSelect: () => void setDecide(placeId, { alwaysAsk: !decide.alwaysAsk }) },
    {
      kind: 'submenu', id: 'decide-confidence', label: 'Decision confidence…',
      items: figures.map(threshold => ({ id: `decide-${threshold}`, label: `${threshold}%`, checked: threshold === decide.threshold, onSelect: () => void setDecide(placeId, { threshold }) })),
    },
  ];
}
