// The engine's worker brief, read by its own structure. The engine composes it
// in internal/session/task_brief.go (composeBriefScoped): each section is a
// SHOUTED heading line, then (optionally) one rule line directly under it, a
// blank line, and the body. The headings below are that file's constants; a
// line is a heading only when it is exactly one of them, so a person's own text
// that happens to be upper case is never cut.

export type BriefSection = { heading: string; rule: string; body: string };
export type ParsedBrief = {
  /** What a person reads first: the task's work, else the person's own words. Empty when the text is not a brief. */
  summary: string;
  /** The brief's own sections, in the engine's order; empty when the text is not a brief. */
  sections: BriefSection[];
};

const ASK = 'WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS';
const WORK = 'THE WORK';
const HEADINGS = new Set([
  ASK,
  WORK,
  'WHAT TO PRODUCE',
  'DONE WHEN',
  'THE FOLDER THIS WORK IS ABOUT, AND YOUR OWN COPY OF IT',
  "THE PERSON'S ORIGINAL MESSAGE",
  'SOME OF WHAT WAS SAID AROUND THIS WORK',
  'CALLS THAT HAVE ALREADY RUN',
  'WHAT THIS BRIEF ASSUMES, AND WAS CHECKED BEFORE YOU STARTED',
]);

/** "WHAT TO PRODUCE" reads "What to produce". */
export function headingLabel(heading: string): string {
  const lower = heading.toLowerCase();
  return lower.charAt(0).toUpperCase() + lower.slice(1);
}

type Raw = { heading: string; lines: string[] };

/** A heading line stands alone: it opens the text or follows a blank line. */
function rawSections(text: string): Raw[] {
  const raws: Raw[] = [];
  let before = '';
  for (const line of text.split('\n')) {
    if (HEADINGS.has(line.trim()) && before.trim() === '') raws.push({ heading: line.trim(), lines: [] });
    else raws[raws.length - 1]?.lines.push(line);
    before = line;
  }
  return raws;
}

function sectionOf({ heading, lines }: Raw): BriefSection {
  // A rule is the one line directly under the heading, before the blank line.
  const hasRule = lines.length > 1 && lines[0].trim() !== '' && lines.some((line) => line.trim() === '');
  return { heading, rule: hasRule ? lines[0].trim() : '', body: (hasRule ? lines.slice(1) : lines).join('\n').trim() };
}

/** Reads the engine's worker brief; text that is not one comes back whole as the summary. */
export function parseBrief(text: string): ParsedBrief {
  const sections = rawSections(text).map(sectionOf);
  if (sections.length === 0) return { summary: text.trim(), sections: [] };
  const own = (heading: string) => sections.find((section) => section.heading === heading)?.body ?? '';
  return { summary: own(WORK) || own(ASK) || text.trim(), sections: sections.filter((section) => section.body) };
}
