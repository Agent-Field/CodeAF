import { projectEngineSections, type EngineSnapshot } from './engine-client';
import type { WorkDocument } from './work-model';
import type { WorkPhase } from './activity';

/** Canonical data updates content; view-only folding and reading position stay local. */
export function engineDocumentChange(value: EngineSnapshot, before: WorkDocument): Partial<WorkDocument> {
 const sections = projectEngineSections(value, before.sections);
 const stamp = Date.parse(value.updatedAt ?? '');
 const phase: WorkPhase = value.needsPerson ? 'waiting' : value.running ? 'working' : before.activity?.phase === 'stopped' || before.activity?.phase === 'failed' ? before.activity.phase : sections.some(section => section.blocks.length) ? 'completed' : 'staged';
 return {
  engineTitle: value.title.trim() || before.engineTitle,
  engine: { sessionFile: value.sessionFile, workspace: value.workspace, model: value.model },
  sections, running: value.running, decision: false, stopped: phase === 'stopped', taskPreview: false,
  activity: sections.length || value.running || value.needsPerson ? { phase, recordedAt: Number.isFinite(stamp) ? stamp : before.activity?.recordedAt ?? Date.now(), sample: false } : undefined,
  notice: value.needsPerson ? value.questions?.length ? 'The engine needs your input below.' : 'The engine needs your input. Open this conversation in the TUI to answer its pending approval or question.' : '',
 };
}
