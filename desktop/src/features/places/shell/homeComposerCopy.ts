import type { HomeView } from '../home-model';

/**
 * The empty place of Places 8b: nothing of its own is listed yet. A decision or a line of knowledge is read
 * separately and can still take the page off this screen; the caller folds those in.
 */
export function homePlaceIsEmpty(view: Pick<HomeView, 'chats' | 'children' | 'attention' | 'sources'>): boolean {
  return view.chats.length === 0 && view.children.length === 0 && view.attention.length === 0 && !view.sources?.length;
}

/** Places 8a names a place that already holds something; 8b names the first chat in an empty one. */
export function homeComposerPlaceholder(placeName: string, empty: boolean): string {
  return empty ? `Start the first chat in ${placeName}` : `Start something in ${placeName}`;
}
