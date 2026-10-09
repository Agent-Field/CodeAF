import type { WorkDocument } from './work-model';
export type WorkPhase = 'streaming' | 'working' | 'waiting' | 'completed' | 'stopped' | 'failed' | 'staged';
export type WorkActivity = { phase: WorkPhase; recordedAt: number; sample: boolean };
export function phaseFor(document?: WorkDocument): WorkPhase | undefined {
 if (!document) return;
 if (document.decision) return 'waiting';
 if (document.stopped) return 'stopped';
 if (document.running) return document.activity?.phase === 'streaming' ? 'streaming' : 'working';
 return document.activity?.phase;
}
const labels: Record<WorkPhase, string> = { streaming: 'Receiving reply', working: 'Working', waiting: 'Needs your input', completed: 'Completed', stopped: 'Stopped', failed: 'Needs attention', staged: 'Staged locally' };
export function activityLabel(document?: WorkDocument): string {
 const phase = phaseFor(document);
 return phase ? `${labels[phase]}${document?.activity?.sample || (!document?.engine && (document?.running || document?.decision)) ? ' · sample' : ''}` : '';
}
export function relativeActivity(recordedAt: number, now: number): string {
 const seconds = Math.max(0, Math.floor((now - recordedAt) / 1000));
 if (seconds < 60) return 'just now';
 const format = new Intl.RelativeTimeFormat(undefined, { numeric: 'always' });
 if (seconds < 3600) return format.format(-Math.floor(seconds / 60), 'minute');
 if (seconds < 86400) return format.format(-Math.floor(seconds / 3600), 'hour');
 return format.format(-Math.floor(seconds / 86400), 'day');
}
export function activityMeta(document: WorkDocument | undefined, now: number): string {
 const label = activityLabel(document);
 return document?.activity ? `${label} · ${relativeActivity(document.activity.recordedAt, now)}` : label;
}
