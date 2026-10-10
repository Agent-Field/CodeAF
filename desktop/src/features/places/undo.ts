// Place actions register engine authority in the shell's window stack. Receipts are never reconstructed from UI
// state: the engine checks whether their inverses still apply, including changes made by another window.
import type { PlaceInverse } from './actions.ts';
import { PlacesError, type PlacesClient } from './client.ts';
import { windowStructuralUndo, type StructuralUndo } from '../tabs/undo/structuralUndo.ts';

export type PlaceUndo = ReturnType<typeof createPlaceUndo>;

/** Injecting the stack lets tests model separate window realms without creating a place-owned ring. */
export function createPlaceUndo(client: Pick<PlacesClient, 'undo'>, stack: StructuralUndo = windowStructuralUndo) {
  return {
    /** Rail closes have no graph receipt; their inverse is the canonical visit that reopens saved tabs. */
    registerClose(reopen: () => Promise<void>) { return stack.register(reopen); },
    register(inverse: Pick<PlaceInverse, 'receipts'>, afterUndo?: () => void | Promise<void>) {
      let receipts = [...inverse.receipts];
      if (!receipts.length) return undefined;
      const entry = stack.register(async () => {
        try {
          await client.undo([...receipts]);
        } catch (failure) {
          // An engine refusal is final; a transport failure retains the exact receipt for an explicit retry.
          if (failure instanceof PlacesError) {
            if (failure.code === 'cannot_undo' || failure.code === 'no_receipt') entry.forget();
            else if (Number.isInteger(failure.undone) && failure.undone! > 0) {
              receipts = receipts.slice(0, Math.max(0, receipts.length - failure.undone!));
              if (!receipts.length) entry.forget();
            }
          }
          throw failure;
        }
        entry.forget();
        await afterUndo?.();
      });
      return entry;
    },
  };
}
