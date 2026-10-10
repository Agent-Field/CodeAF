// Every sentence a web tab says about its own state, in one place so the pane,
// the tab tooltip and the tests read the same words. No state is spelled in
// colour: the sentence is ink, and only the 6px glyph beside it carries hue.

import type { WebErrorKind } from './model.ts';

export const webStateSentence: Record<WebErrorKind, { title: string; detail: (site: string) => string } | null> = {
  none: null,
  offline: { title: 'You are offline', detail: () => 'Check your connection, then try again.' },
  blocked: { title: 'This page is blocked', detail: site => `${site} refused to open here.` },
  certificate: { title: 'This site is not secure', detail: site => `The certificate for ${site} could not be verified.` },
  failed: { title: 'This page did not load', detail: site => `codeaf could not reach ${site}.` },
};

export const webLoadingLabel = 'Loading';
export const webUntitled = 'New tab';
