import { useCallback, useEffect, useState } from 'react';
import { MODELS_CHANGED, readModelCatalog, readModelRoles, readPinnedModels, readPlacesPolicy, setModelRole, setPinnedModels, setPlacesPolicy, type CatalogModel, type ModelRole, type ModelRoleCategory, type PinnedModel, type PlacesSetting } from '../chat/engine-client';

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

  useEffect(() => {
    let live = true;
    Promise.all([readModelCatalog(), readModelRoles(), readPinnedModels()]).then(([listed, rows, pins]) => {
      if (!live) return;
      setCatalog(listed.models);
      setRoles(rows.roles);
      setCategories(rows.categories);
      setPinned(pins.pinned);
      setPinsChosen(pins.chosen);
      setState('ready');
    }).catch(() => { if (live) setState('unavailable'); });
    // Read on its own: an engine without the Places routes still shows every model choice.
    readPlacesPolicy().then(settings => { if (live) setPlaces({ state: 'ready', settings }); })
      .catch(() => { if (live) setPlaces({ state: 'unavailable', settings: [] }); });
    return () => { live = false; };
  }, []);

  const saved = useCallback(() => {
    setReceipt({ text: RECEIPT, failed: false });
    window.dispatchEvent(new Event(MODELS_CHANGED));
  }, []);
  const failed = useCallback((error: unknown) => setReceipt({ text: error instanceof Error ? `Not saved · ${error.message}` : 'Not saved', failed: true }), []);

  const saveRole = useCallback((id: string, choice: { model: string; effort?: string }) => {
    setModelRole(id, choice).then(row => { setRoles(rows => rows.map(role => (role.id === id ? row : role))); saved(); }).catch(failed);
  }, [saved, failed]);
  const writePins = useCallback((models: string[]) => {
    setPinnedModels(models).then(view => { setPinned(view.pinned); setPinsChosen(view.chosen); saved(); }).catch(failed);
  }, [saved, failed]);
  const savePin = useCallback((slot: number, model: string) => writePins(pinSlot(pinned.map(entry => entry.id), slot, model)), [pinned, writePins]);
  const resetPins = useCallback(() => writePins([]), [writePins]);

  const savePlaces = useCallback((key: string, value: boolean | number | null) =>
    setPlacesPolicy(key, value).then(row => {
      setPlaces(view => ({ ...view, settings: view.settings.map(setting => (setting.key === key ? row : setting)) }));
      setReceipt({ text: RECEIPT, failed: false });
      return true;
    }, (error: unknown) => { failed(error); return false; }), [failed]);

  return { state, catalog, roles, categories, pinned, places, pinsChosen, receipt, saveRole, savePin, resetPins, savePlaces };
}
