import type { Page } from '@playwright/test';

// The conversation mock does not own the Places graph. The Places mock registers this lookup so a new
// chat opened with placeId is born in that place's first usable folder, the same rule as the bridge.
const folders = new WeakMap<Page, (placeId: string) => string | undefined>();

/** Remembers where a page's places mock says a new chat in that place should work. */
export function bindPlaceFolders(page: Page, lookup: (placeId: string) => string | undefined): void {
  folders.set(page, lookup);
}

/** The first folder or repo the place lists, or undefined when it lists none the chat can use. */
export function firstPlaceFolder(page: Page, placeId: string): string | undefined {
  return folders.get(page)?.(placeId);
}
