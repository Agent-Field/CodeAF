import { keyStatus, type KeySource } from './settingsClient.ts';

/** The only sentences the row can say; each is chosen by source, never built from anything the engine sent. */
const SOURCE_WORDS: Record<KeySource, string> = {
  OPENROUTER_API_KEY: 'From OPENROUTER_API_KEY',
  OPENAI_API_KEY: 'From OPENAI_API_KEY',
  profile: 'Saved in your profile',
};

/**
 * The words for the provider key row, or null when the engine could not say. A failed or malformed answer is unknown,
 * and unknown renders as nothing (emptiness law). A present key with no recognized source also stays unknown.
 */
export async function keyStatusWords(): Promise<string | null> {
  try {
    const status = await keyStatus();
    if (!status.present) return 'Not set';
    return status.source ? SOURCE_WORDS[status.source] : null;
  } catch {
    return null;
  }
}
