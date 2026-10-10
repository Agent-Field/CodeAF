import tokens from '../../../design/tokens.json' with { type: 'json' };

/** The engine's Line shape keeps provenance separate from editable words. */
export type KnowsLine = {
  id: string;
  placeId: string;
  text: string;
  source: { kind: 'you-wrote' | 'said-in-chat' | 'learned' | 'file'; chatId?: string; at?: string; answers?: number; path?: string };
  createdAt?: string;
  lastUsedAt?: string;
  replacedBy?: string;
  replacedAt?: string;
  askedStillTrueAt?: string;
};

export type KnowsRow = { line: KnowsLine; source: string; struck: boolean; prompt: string };
export type KnowsList = { home: KnowsRow[]; all: KnowsRow[]; allLabel: string };

const DAY_MS = 86_400_000;
const rules = tokens.interaction;
const weekdays = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

/** Missing and invalid dates remain unknown, including Go's zero timestamp. */
function instant(value?: string): number | undefined {
  if (!value || value.startsWith('0001-01-01')) return undefined;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function weekday(value?: string): string {
  const at = instant(value);
  return at === undefined ? '' : weekdays[new Date(at).getDay()];
}

function learnedAt(line: KnowsLine): number | undefined {
  return instant(line.createdAt) ?? instant(line.source.at);
}

/** Chat names come from the caller's real titles, never from identifiers. */
export function sourceWords(line: KnowsLine, chatTitles: Readonly<Record<string, string>> = {}): string {
  if (line.replacedBy) {
    const day = weekday(line.replacedAt);
    return day ? `Replaced ${day} · kept for ${rules.knowsReplacedDays} days` : '';
  }
  switch (line.source.kind) {
    case 'you-wrote': return 'You wrote';
    case 'said-in-chat': {
      const title = line.source.chatId ? chatTitles[line.source.chatId]?.trim() : undefined;
      const day = weekday(line.source.at);
      return title ? `You said in ${title}${day ? ` · ${day}` : ''}` : '';
    }
    case 'learned': {
      const count = line.source.answers;
      return count && Number.isInteger(count) && count > 0
        ? `Learned from ${count} of your ${count === 1 ? 'answer' : 'answers'}` : '';
    }
    case 'file': {
      const path = line.source.path?.trim();
      const name = path?.split(/[\\/]/).pop();
      return name ? `${name} · you added` : '';
    }
  }
}

/** Match the engine's stale rule, including its recorded sixty-day ask cooldown. */
export function stillTrueDue(line: KnowsLine, now: Date): boolean {
  if (line.replacedBy || !Number.isFinite(now.getTime())) return false;
  const last = instant(line.lastUsedAt) ?? learnedAt(line);
  const age = rules.knowsUnusedDays * DAY_MS;
  if (last === undefined || now.getTime() - last < age) return false;
  const asked = instant(line.askedStillTrueAt);
  return asked === undefined || now.getTime() - asked >= age;
}

/** The complete list and Home share one ordering and one injected clock. */
export function knowsList(lines: readonly KnowsLine[], now: Date, chatTitles: Readonly<Record<string, string>> = {}): KnowsList {
  const all = lines.filter(line => {
    if (!line.replacedBy) return true;
    const at = instant(line.replacedAt);
    // Unknown replacement age cannot justify hiding a recorded fact.
    return at === undefined || !Number.isFinite(now.getTime()) || now.getTime() - at < rules.knowsReplacedDays * DAY_MS;
  }).sort((a, b) => {
    const used = (instant(b.lastUsedAt) ?? -Infinity) - (instant(a.lastUsedAt) ?? -Infinity);
    if (used && !Number.isNaN(used)) return used;
    // Stored order breaks exact ties without inventing a preference from text or ids.
    return (learnedAt(b) ?? -Infinity) - (learnedAt(a) ?? -Infinity) || 0;
  }).map(line => ({ line, source: sourceWords(line, chatTitles), struck: Boolean(line.replacedBy), prompt: stillTrueDue(line, now) ? 'still true?' : '' }));
  return { home: all.slice(0, rules.knowsHomeLimit), all, allLabel: all.length > rules.knowsHomeLimit ? 'All' : '' };
}
