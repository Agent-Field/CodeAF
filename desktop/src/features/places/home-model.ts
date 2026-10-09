import type { TintName } from './components/PlaceSwatch';
import type { AttentionStatus } from './components/AttentionRow';
import type { ChatStatus } from './components/ChatRow';

/** The view model a Places Home draws. It mirrors the fields of the published read `GET /places/{id}` (run/places-routes-api.md): a
 * caller maps the typed client's digest onto it in one place, and nothing here is invented. Absent means unknown, and unknown draws nothing. */
export type HomeKind = 'place' | 'root' | 'now';

export type HomeCrumb = { id: string; name: string };

export type HomeChild = {
  id: string;
  name: string;
  /** The effective tint (its own, or the first parent's). */
  tint: TintName;
  tintSource?: 'own' | 'inherited';
  /** Places inside it (descendants) and chats inside it or under it. */
  places: number;
  chats: number;
  /** The names of the other parents past the first: "Release · also in Software". */
  alsoIn?: readonly string[];
  /** A descendant needs you (amber) or failed (red). Running never shows on a tile. */
  status?: 'waiting' | 'failed';
  pinned?: boolean;
  archived?: boolean;
  /** Only for a search across every place: where it sits ("codeaf › Software"). */
  path?: readonly string[];
};

export type HomeAttention = {
  /** The chat or task id this row opens. */
  id: string;
  title: string;
  /** The place it lives in when that is not the place being viewed. */
  placeName?: string;
  status: AttentionStatus;
  /** "running · 2m": the engine's own words, otherwise the plain status word. */
  statusText?: string;
};

export type HomeChat = {
  id: string;
  /** Empty means unnamed: it is never invented. */
  title: string;
  excerpt?: string;
  status?: ChatStatus;
  /** ISO instant of the last word. */
  at?: string;
  model?: string;
};

/** A source a place keeps (the published `SourceView`): where it points and whether the stat-only check found it. Nothing is read or fetched to draw it. */
export type HomeSource = { id: string; kind: 'folder' | 'repo' | 'file' | 'url' | 'chat'; label: string; state?: 'ok' | 'missing' | 'unreadable' | 'unknown' };

export type HomeView = {
  kind: HomeKind;
  /** `root`, `now`, or the place id. */
  id: string;
  title: string;
  tint: TintName;
  tintSource?: 'own' | 'inherited';
  /** Ancestors, outermost first, ending at the parent. The root draws none; the "All places" crumb is added for a place. */
  breadcrumb: readonly HomeCrumb[];
  /** Engine-written recap text only, never generated here. */
  recap?: { label: string; text: string };
  attention: readonly HomeAttention[];
  children: readonly HomeChild[];
  archivedChildren?: readonly HomeChild[];
  chats: readonly HomeChat[];
  /** More chats exist than the read returned (the digest caps at 100). */
  chatsTruncated?: boolean;
  /** A place's own sources. The section is absent when there are none. */
  sources?: readonly HomeSource[];
  /** "Uses Marketing's context: brand-voice.md, codeaf.dev": the engine's line for a place that inherits. */
  contextLine?: string;
  pinned?: boolean;
  /** Root only: every place in the graph, for the count line and for search. */
  totals?: { topLevel: number; all: number };
  allPlaces?: readonly HomeChild[];
  /** Root only: chats in no place, and an engine-made offer to move a cluster of them. */
  unplaced?: { total: number };
  suggestion?: { text: string; action: string };
};

/** What the page is doing about its read. `offline` with a view keeps the last good page, read-only. */
export type HomeConnection = { state: 'ready' } | { state: 'loading' } | { state: 'error'; message: string } | { state: 'offline'; message?: string };

export type HomeDeleteImpact = { children: number; chatsHere: number; wouldBeUnplaced: readonly string[] };

const plural = (count: number, one: string, many = `${one}s`) => `${count} ${count === 1 ? one : many}`;

/** "28 chats", "3 places · 19 chats", "6 chats · also in Software". Zero and unknown are left out (the emptiness law). */
export function childMeta(child: Pick<HomeChild, 'places' | 'chats' | 'alsoIn' | 'path'>): string {
  const parts: string[] = [];
  if (child.path && child.path.length) parts.push(`in ${child.path.join(' › ')}`);
  if (child.places > 0) parts.push(plural(child.places, 'place'));
  if (child.chats > 0) parts.push(plural(child.chats, 'chat'));
  if (child.alsoIn && child.alsoIn.length) parts.push(`also in ${child.alsoIn.join(', ')}`);
  return parts.join(' · ');
}

const weekdays = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const startOfDay = (date: Date) => new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();

/** The short time at a chat row's right edge. `now` is injected so a test never sleeps or depends on the clock:
 * minutes and hours today, "Yesterday", the weekday inside a week, "Last wk", then the date. Unparseable or absent draws nothing. */
export function shortTime(iso: string | undefined, now: Date): string {
  if (!iso) return '';
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return '';
  const age = now.getTime() - at.getTime();
  if (age < 0) return 'Today';
  const days = Math.round((startOfDay(now) - startOfDay(at)) / 86_400_000);
  if (days === 0) {
    const minutes = Math.floor(age / 60_000);
    if (minutes < 1) return 'Now';
    if (minutes < 60) return `${minutes}m`;
    return `${Math.floor(minutes / 60)}h`;
  }
  if (days === 1) return 'Yesterday';
  if (days < 7) return weekdays[at.getDay()];
  if (days < 14) return 'Last wk';
  return `${months[at.getMonth()]} ${at.getDate()}`;
}

/** Case-insensitive match across name and path, in the order given. */
export function searchPlaces(places: readonly HomeChild[], query: string): HomeChild[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return [...places];
  return places.filter(place => `${place.name} ${(place.path ?? []).join(' ')}`.toLowerCase().includes(needle));
}

/** The count line under "All places": "5 at the top level · 58 in all". */
export function totalsLine(totals: { topLevel: number; all: number } | undefined): string {
  if (!totals || totals.all <= 0) return '';
  return totals.all === totals.topLevel ? `${totals.all} in all` : `${totals.topLevel} at the top level · ${totals.all} in all`;
}

/** A name a person typed is usable when something is in it and no sibling already has it (case-insensitively, like the store). */
export function nameProblem(name: string, siblings: readonly string[], ignore?: string): string | undefined {
  const value = name.trim();
  if (!value) return 'A place needs a name.';
  if (value.length > 120) return 'Names are at most 120 characters.';
  const clash = siblings.some(sibling => sibling.toLowerCase() === value.toLowerCase() && sibling.toLowerCase() !== ignore?.toLowerCase());
  return clash ? `There is already a place called “${value}” here.` : undefined;
}
