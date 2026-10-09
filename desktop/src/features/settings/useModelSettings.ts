import { useCallback, useEffect, useRef, useState } from 'react';
import { MODELS_CHANGED, readModelCatalog, readModelRoles, readPinnedModels, readPlacesPolicy, setModelRole, setPinnedModels, setPlacesPolicy, type CatalogModel, type ModelRole, type ModelRoleCategory, type PinnedModel, type PlacesSetting } from '../chat/engine-client';

import { createSaveQueue } from './saveQueue';

export const RECEIPT = 'Saved · applies to the next call';

export type ModelSettings = {
  state: 'loading' | 'ready' | 'unavailable';
  catalog: CatalogModel[];
  roles: ModelRole[];
  categories: ModelRoleCategory[] | undefined;
  pinned: PinnedModel[];
  /** The Places organization choices; 'unavailable' when this engine does not serve them yet. */
  places: { state: 'loading' | 'ready' | 'unavailable'; settings: PlacesSetting[] };
  pinsChosen: boolean;
  /** The last save's receipt, or the reason it failed; empty before the first change. */
  receipt: { text: string; failed: boolean } | undefined;
  saveRole: (id: string, choice: { model: string; effort?: string }) => void;
  /** Puts a model in one pinned slot; a model already pinned elsewhere trades places with it. */
  savePin: (slot: number, model: string) => void;
  resetPins: () => void;
  /** Saves one Places setting; `null` puts it back on its default. Resolves false when it was not saved. */
  savePlaces: (key: string, value: boolean | number | null) => Promise<boolean>;
};

/** The pinned list after one slot takes a model: the old occupant of that model's other slot takes the vacated one. */
export function pinSlot(pinned: readonly string[], slot: number, model: string): string[] {
  const next = [...pinned];
  const other = next.indexOf(model);
  if (other >= 0) next[other] = next[slot];
  next[slot] = model;
  return next;
}

/** Everything the Models settings page reads and writes, through the engine's one door for model choices. */
export function useModelSettings(): ModelSettings {
  const [state, setState] = useState<ModelSettings['state']>('loading');
  const [catalog, setCatalog] = useState<CatalogModel[]>([]);
  const [roles, setRoles] = useState<ModelRole[]>([]);
  const [categories, setCategories] = useState<ModelRoleCategory[]>();
  const [places, setPlaces] = useState<ModelSettings['places']>({ state: 'loading', settings: [] });
  const [pinned, setPinned] = useState<PinnedModel[]>([]);
  const [pinsChosen, setPinsChosen] = useState(false);
  const [receipt, setReceipt] = useState<ModelSettings['receipt']>();
  const [queue] = useState(createSaveQueue);
  const active = useRef(new Map<string, { signature: string; result: Promise<boolean>; version: number }>());
  const versions = useRef(new Map<string, number>());
  const failures = useRef(new Map<string, string>());
  // A queued pinned-slot change starts from the previous save's answer, not an old render's list.
  const savedPins = useRef<string[]>([]);

  useEffect(() => {
    let live = true;
    Promise.all([readModelCatalog(), readModelRoles(), readPinnedModels()]).then(([listed, rows, pins]) => {
      if (!live) return;
      setCatalog(listed.models);
      setRoles(rows.roles);
      setCategories(rows.categories);
      savedPins.current = pins.pinned.map(model => model.id);
      setPinned(pins.pinned);
      setPinsChosen(pins.chosen);
      setState('ready');
    }).catch(() => { if (live) setState('unavailable'); });
    // Read on its own: an engine without the Places routes still shows every model choice.
    readPlacesPolicy().then(settings => { if (live) setPlaces({ state: 'ready', settings }); })
      .catch(() => { if (live) setPlaces({ state: 'unavailable', settings: [] }); });
    return () => { live = false; };
  }, []);

  const showReceipt = useCallback(() => {
    const errors = [...failures.current.values()];
    const error = errors[errors.length - 1];
    setReceipt(error
      ? { text: error, failed: true }
      : { text: active.current.size ? 'Saving…' : RECEIPT, failed: false });
  }, []);

  /** Keep the last intent visible. A duplicate Enter/blur shares its save, and a failed save never blocks retry. */
  const write = useCallback(<T,>(key: string, signature: string, request: () => Promise<T>, apply: (answer: T) => void): Promise<boolean> => {
    const held = active.current.get(key);
    if (held?.signature === signature) return held.result;
    const version = (versions.current.get(key) ?? 0) + 1;
    versions.current.set(key, version);
    failures.current.delete(key);
    const result = queue.enqueue(key, signature, async () => {
      try {
        const answer = await request();
        if (versions.current.get(key) === version) {
          apply(answer);
          failures.current.delete(key);
          window.dispatchEvent(new Event(MODELS_CHANGED));
        }
        return true;
      } catch (error: unknown) {
        if (versions.current.get(key) === version) failures.current.set(key,
          error instanceof Error ? `Not saved · ${error.message}` : 'Not saved');
        return false;
      } finally {
        if (versions.current.get(key) === version) {
          active.current.delete(key);
          showReceipt();
        }
      }
    });
    active.current.set(key, { signature, result, version });
    showReceipt();
    return result;
  }, [queue, showReceipt]);

  const saveRole = useCallback((id: string, choice: { model: string; effort?: string }) => {
    void write(`role:${id}`, JSON.stringify(choice), () => setModelRole(id, choice),
      row => setRoles(rows => rows.map(role => (role.id === id ? row : role))));
  }, [write]);
  const writePins = useCallback((signature: string, change: (saved: readonly string[]) => string[]) => {
    void write('pins', signature, async () => {
      const view = await setPinnedModels(change(savedPins.current));
      savedPins.current = view.pinned.map(model => model.id);
      return view;
    }, view => { setPinned(view.pinned); setPinsChosen(view.chosen); });
  }, [write]);
  const savePin = useCallback((slot: number, model: string) =>
    writePins(JSON.stringify({ slot, model }), saved => pinSlot(saved, slot, model)), [writePins]);
  const resetPins = useCallback(() => writePins('reset', () => []), [writePins]);

  const savePlaces = useCallback((key: string, value: boolean | number | null) =>
    write(`places:${key}`, JSON.stringify(value), () => setPlacesPolicy(key, value),
      row => setPlaces(view => ({ ...view, settings: view.settings.map(setting => (setting.key === key ? row : setting)) }))), [write]);

  return { state, catalog, roles, categories, pinned, places, pinsChosen, receipt, saveRole, savePin, resetPins, savePlaces };
}
