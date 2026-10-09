// The group suggestion's state: which offer is showing, what this launch has already shown, and what was decided.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { findOffers, nextOffer, readMemory, remember, writeMemory, type GroupOffer, type OfferMemory } from './offerRules.ts';
import type { Tab } from './types.ts';

export type GroupOfferState = {
  /** The suggestion to draw now, with its members as they stand this moment; absent when there is none. */
  offer: GroupOffer | undefined;
  /** Group: remembers the set as decided and hands the members to `onGroup`. */
  accept: () => void;
  /** The X: remembers the set as decided for the memory's length and draws nothing more. */
  dismiss: () => void;
};

type Options = {
  tabs: readonly Tab[];
  /** The ONE way a group is made: the caller dispatches the reducer's own `group` action. Nothing here builds a group. */
  onGroup: (ids: string[], title?: string) => void;
  /** Drawn nothing while a modal layer (switcher, overview, rename) is open. The offer is kept, not spent. */
  suspended?: boolean;
  storage?: Storage;
  now?: () => number;
};

const defaultStorage = () => (typeof localStorage === 'undefined' ? undefined : localStorage);

/** A set is shown at most once per launch (`shown`), and never again once decided (the persisted memory). */
export function useGroupOffer({ tabs, onGroup, suspended = false, storage = defaultStorage(), now = Date.now }: Options): GroupOfferState {
  const [memory, setMemory] = useState<OfferMemory>(() => readMemory(storage, now()));
  const shown = useRef(new Set<string>());
  const [currentKey, setCurrentKey] = useState<string>();
  const offers = useMemo(() => findOffers(tabs), [tabs]);
  const live = offers.find(offer => offer.key === currentKey && !(offer.key in memory));

  useEffect(() => {
    if (live || suspended) return;
    const next = nextOffer(offers, memory, shown.current);
    if (next) { shown.current.add(next.key); setCurrentKey(next.key); } else if (currentKey) setCurrentKey(undefined);
  }, [live, suspended, offers, memory, currentKey]);

  const decide = useCallback((key: string) => {
    setMemory(current => { const next = remember(current, key, now()); writeMemory(storage, next); return next; });
    setCurrentKey(undefined);
  }, [storage, now]);
  const offer = suspended ? undefined : live;
  return {
    offer,
    accept: () => { if (!offer) return; onGroup(offer.ids, offer.title); decide(offer.key); },
    dismiss: () => { if (offer) decide(offer.key); },
  };
}
