import { object, invalid } from '../decisions/client.ts';
import type { DecideStatus } from '../decisions/StatusLine';
import type { DecidedItem } from '../decisions/decidedModel';

/** The bridge owns the decision words; Home only maps its ledger fields to the shared row. */
export function homeDecisions(value: unknown): DecidedItem[] {
  if (!object(value) || !Array.isArray(value.decisions)) return invalid('Home decisions');
  return value.decisions.map((row: unknown) => {
    if (!object(row) || typeof row.id !== 'string' || typeof row.action !== 'string' || typeof row.at !== 'string') return invalid('Home decision');
    return {
      id: row.id, title: row.action, at: row.at,
      detail: typeof row.because === 'string' ? row.because : undefined,
      why: {
        by: typeof row.byName === 'string' ? row.byName : undefined,
        because: typeof row.because === 'string' ? row.because : undefined,
        sure: typeof row.percent === 'number' && row.percent > 0 && row.percent <= 100 ? `${row.percent}%` : undefined,
      },
    };
  });
}

/** Unknown modes and malformed counts cannot become an apparently real status. */
export function homeStatus(value: unknown): DecideStatus {
  if (!object(value) || !['none', 'learning', 'deciding', 'always-ask'].includes(value.mode as string)) return invalid('Home status');
  for (const key of ['agreedWeek', 'totalWeek']) {
    if (value[key] !== undefined && (!Number.isSafeInteger(value[key]) || (value[key] as number) < 0)) return invalid('Home status');
  }
  if (value.learning !== undefined && (!object(value.learning) || !Number.isSafeInteger(value.learning.agreed) || !Number.isSafeInteger(value.learning.of)
    || (value.learning.agreed as number) < 0 || (value.learning.of as number) < (value.learning.agreed as number))) return invalid('Home learning status');
  return value as DecideStatus;
}
