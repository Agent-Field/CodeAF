// The group suggestion's state: which offer is showing, what this launch has already shown, and what was decided.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { findOffers, looseChatIDs, mergeOffers, nextOffer, offerSettleMs, readMemory, remember, shouldAskCanonical, tabOffersForChats, writeMemory, canonicalSetKey, type ChatSummary, type GroupOffer, type OfferMemory } from './offerRules.ts';
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
  /** Engine summaries keyed by tab id. Canonical saved chat identity is separate from its transient bridge token. */
  summaries?: Readonly<Record<string, ChatSummary | undefined>>;
  /**
   * Asks the engine about one settled set of canonical chat ids. Absent in a specimen that has no engine.
   * The ids it returns are chat ids; this hook maps them onto tabs.
   */
  ask?: (ids: readonly string[]) => Promise<readonly GroupOffer[]>;
  /** How long the set must sit still. The default is [offerSettleMs]. */
  settleMs?: number;
  /** Drawn nothing while a modal layer (switcher, overview, rename) is open. The offer is kept, not spent. */
  suspended?: boolean;
  storage?: Storage;
  now?: () => number;
};

const defaultStorage = () => (typeof localStorage === 'undefined' ? undefined : localStorage);

/** A set is shown at most once per launch (`shown`), and never again once decided (the persisted memory). */
export function useGroupOffer({ tabs, onGroup, summaries = {}, ask, settleMs = offerSettleMs, suspended = false, storage = defaultStorage(), now = Date.now }: Options): GroupOfferState {
  const [memory, setMemory] = useState<OfferMemory>(() => readMemory(storage, now()));
  const shown = useRef(new Set<string>());
  const asked = useRef(new Set<string>());
  const [currentKey, setCurrentKey] = useState<string>();
  const [raw, setRaw] = useState<readonly GroupOffer[]>([]);
  const chatIDs = useMemo(() => looseChatIDs(tabs, summaries), [tabs, summaries]);
  const setKey = canonicalSetKey(chatIDs);
  const idsRef = useRef(chatIDs);
  idsRef.current = chatIDs;
  // The effect follows the SET, not the tab array. A selection, a draft or another tab that does not change which
  // chats are open leaves an in-flight question alone, and does not start a second one.
  useEffect(() => {
    const ids = idsRef.current;
    if (!ask || !shouldAskCanonical(ids, asked.current)) {
      if (ids.length < 3) setRaw(current => current.length ? [] : current);
      return;
    }
    let liveCall = true;
    const timer = window.setTimeout(() => {
      if (!liveCall || !shouldAskCanonical(ids, asked.current)) return;
      asked.current.add(setKey);
      ask(ids).then(offers => { if (liveCall) setRaw(offers); }).catch(() => { if (liveCall) setRaw([]); });
    }, settleMs);
    return () => { liveCall = false; window.clearTimeout(timer); };
  }, [ask, setKey, settleMs]);
  const offers = useMemo(() => mergeOffers(findOffers(tabs), tabOffersForChats(tabs, summaries, raw)), [tabs, summaries, raw]);
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
