import type { OutgoingFile } from '../../chat/engine-client';

// Mirrors the bridge so a refusal happens here, naming the file, not as a failed send.
export const MAX_PICTURE_BYTES = 10 * 1024 * 1024;
export const MAX_TOTAL_BYTES = 20 * 1024 * 1024;

export type Attachment = {
  id: string;
  file: File;
  picture: boolean;
  /** Object URL for pictures only; the owner revokes it. */
  previewUrl?: string;
};

export type AddResult = { accepted: File[]; error?: string };

export function isPicture(file: File) {
  return file.type.startsWith('image/');
}

export function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function refusal(file: File, used: number): string | undefined {
  if (isPicture(file) && file.size > MAX_PICTURE_BYTES) {
    return `${file.name} is over 10 MB, the limit for a picture.`;
  }
  if (used + file.size > MAX_TOTAL_BYTES) {
    return `${file.name} would take the attachments past 20 MB.`;
  }
  return undefined;
}

/** Takes files in order; the first refusal stops the rest so the message names one file. */
export function admit(existing: Attachment[], incoming: File[]): AddResult {
  let used = existing.reduce((sum, item) => sum + item.file.size, 0);
  const accepted: File[] = [];
  for (const file of incoming) {
    const error = refusal(file, used);
    if (error) return { accepted, error };
    used += file.size;
    accepted.push(file);
  }
  return { accepted };
}

async function encode(file: File): Promise<string> {
  const bytes = new Uint8Array(await file.arrayBuffer());
  let binary = '';
  const step = 0x8000;
  for (let i = 0; i < bytes.length; i += step) {
    binary += String.fromCharCode(...bytes.subarray(i, i + step));
  }
  return btoa(binary);
}

/** Base64 happens only here, at send time. */
export function toOutgoing(items: Attachment[]): Promise<OutgoingFile[]> {
  return Promise.all(
    items.map(async ({ file }) => ({
      name: file.name,
      mime: file.type || 'application/octet-stream',
      dataBase64: await encode(file),
    })),
  );
}
