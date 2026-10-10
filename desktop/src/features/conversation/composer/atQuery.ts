// The @ file reference, as data. Conversation 1e never draws the picker (I-C1e-64/65);
// this is the part the field can decide without one: which @ the caret is in, what to
// ask the engine, and where the chosen path lands. The list itself is a later lane.

/** One row of findEngineFiles: a workspace-relative path split into name and folder. */
export type AtFile = { path: string; name: string; dir: string };

/**
 * The @ the caret is standing in. `at` is the trigger, `end` is the first whitespace
 * after it (or the end of the field), and `query` is only what sits between the trigger
 * and the caret. The suffix still inside the token is the same reference.
 */
export type AtQuery = { at: number; end: number; query: string };

function isWhitespace(char: string): boolean {
  return /\s/.test(char);
}

/**
 * The active @token, or nothing.
 *
 * A token begins at the start of the field or right after whitespace. An @ in the
 * middle of a word — an email, a Go doc link — does not open one, so the walk keeps
 * going: a path may itself contain @ (`@packages/@scope/name`) and the trigger is the
 * @ that begins the word. The caret has to sit after that trigger and inside the run;
 * a space ends it, which is what closes the list when the person types on.
 */
export function activeAt(text: string, caret: number): AtQuery | null {
  if (!Number.isInteger(caret) || caret < 0 || caret > text.length) return null;
  let at = -1;
  for (let i = caret - 1; i >= 0; i--) {
    const char = text[i];
    if (char === '@' && (i === 0 || isWhitespace(text[i - 1]))) {
      at = i;
      break;
    }
    if (isWhitespace(char)) return null;
  }
  if (at < 0) return null;
  let end = caret;
  while (end < text.length && !isWhitespace(text[end])) end++;
  return { at, end, query: text.slice(at + 1, caret) };
}

/**
 * Replace the whole token with the chosen relative path and leave the caret just
 * after it. The @ goes with the token: the design does not draw a chip, and the
 * path the engine returns is already workspace-relative. Text outside the token stays.
 * Nothing changes when the caret is not in a token (an email must not be rewritten).
 */
export function applyAtPath(text: string, caret: number, path: string): { text: string; caret: number } | null {
  const token = activeAt(text, caret);
  if (!token) return null;
  return {
    text: text.slice(0, token.at) + path + text.slice(token.end),
    caret: token.at + path.length,
  };
}

/**
 * Name-prefix matches first, and otherwise the engine's own order.
 *
 * findEngineFiles already returns best-first. This only lifts a row whose file name
 * starts the query, so a person typing the name sees that file above a path that
 * merely contains the same letters. Matching is case-insensitive, the same fold the
 * engine uses. An empty query is not a search: every name would be a prefix of it,
 * so the engine's order is kept. Rows that are not prefixes stay; ranking does not drop them.
 */
export function rankAtFiles(files: readonly AtFile[], query: string): AtFile[] {
  const needle = query.toLowerCase();
  if (!needle) return files.slice();
  const prefix: AtFile[] = [];
  const rest: AtFile[] = [];
  for (const file of files) {
    if (file.name.toLowerCase().startsWith(needle)) prefix.push(file);
    else rest.push(file);
  }
  return prefix.concat(rest);
}
