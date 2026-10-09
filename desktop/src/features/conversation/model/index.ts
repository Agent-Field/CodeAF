// The v2 conversation model: canonical records -> TurnV2[].
export { projectTurnsV2 } from './project.ts';
export { emptyLive, reduceLive } from './live.ts';
export type { LiveCall, LiveOverlayV2, LiveSteer } from './live.ts';
export type { QuestionOutcome, RichSnapshot } from './entry.ts';
export { digestOf } from '../transcript-parse.ts';
