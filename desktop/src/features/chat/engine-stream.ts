import type { WorkSection } from './work-model';
/** Overlay only the current assistant segment. Previously recorded text/tools remain intact. */
export function withStreamingText(section: WorkSection, text: string, baseBlockCount: number): WorkSection {
 const blocks = [...section.blocks];
 const recorded = blocks[baseBlockCount];
 // A canonical snapshot may already contain more of this segment than the reader has received.
 const output = recorded?.kind === 'paragraph' && recorded.text.startsWith(text) ? recorded.text : text;
 blocks[baseBlockCount] = { kind: 'paragraph', text: output };
 const timeline = section.timeline ? [...section.timeline] : undefined;
 if (timeline && !timeline.some(item => item.kind === 'text' && item.index === baseBlockCount)) timeline.push({ kind: 'text', index: baseBlockCount });
 return { ...section, blocks, timeline };
}
