// The engine's read hand-off notes (internal/session/readhandoff.go,
// sweepHandoffWord and sweepCoveredWord). When a batch of reads goes to a quick
// task, the first call's result is "[This reading was handed to quick task N …]"
// then the distilled answer, and every other call's result is the one bracketed
// "[Covered by the handoff to quick task N …]" line. The brackets are session
// notes written for the model, not tool output: the record carries no separate
// field for them, so the leading bracketed paragraph is the engine's own marker.

const NOTE = /^\[(?:This reading was handed to|Covered by the handoff to) quick task \d+[^\]]*\]\s*/;

export type Handoff = { output: string; covered: boolean };

/** The output without its hand-off note; covered when a note was all there was. */
export function withoutHandoff(output: string): Handoff {
  const match = NOTE.exec(output);
  if (!match) return { output, covered: false };
  const rest = output.slice(match[0].length);
  return { output: rest, covered: rest.trim() === '' };
}
