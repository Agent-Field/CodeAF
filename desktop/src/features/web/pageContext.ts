// "Start a conversation with this page": what is handed to a new conversation,
// built only when the person presses the button. It is the page's address and
// title as the view reported them, and, where the platform can take one, the
// picture of the page captured at that moment. Nothing here fetches the page
// or calls a model: the person reads the unsent draft and sends it, and the
// engine receives the attachment through its ordinary path.

export type PageContext = { url: string; title: string; shot?: string };

export type PageAttachment = { draft: string; files: File[] };

/** The draft a new conversation starts with; the person edits or sends it. */
export function pageDraft(page: PageContext): string {
  const title = page.title.trim();
  return title ? `About this page, "${title}": ${page.url}\n\n` : `About this page: ${page.url}\n\n`;
}

function pngFile(dataUrl: string, name: string): File | null {
  const match = /^data:image\/png;base64,([a-z0-9+/=]+)$/i.exec(dataUrl);
  if (!match) return null;
  const binary = atob(match[1]);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return new File([bytes], name, { type: 'image/png' });
}

/** The draft plus the captured picture as an ordinary composer attachment. */
export function pageAttachment(page: PageContext): PageAttachment {
  const shot = page.shot ? pngFile(page.shot, 'page.png') : null;
  return { draft: pageDraft(page), files: shot ? [shot] : [] };
}
