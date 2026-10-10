export type NextUpProgress = { index: number; total: number };

/** The walker owns progress; a header without a live walk draws nothing. */
export function NextUpChip({ progress }: { progress?: NextUpProgress }) {
  if (!progress || !Number.isInteger(progress.index) || !Number.isInteger(progress.total) || progress.index < 1 || progress.index > progress.total) return null;
  return <span className="next-up-chip">Next up<span className="next-up-chip-progress">{progress.index} of {progress.total}</span></span>;
}
