import type { ReactNode } from 'react';
import design from '../../design/tokens.json';
import { CodeText, Icon, SectionHeading, StatusMark, type Status } from '../ui';
import { Shimmer } from '../ui/Shimmer';
import { BreathingDot } from '../ui/BreathingDot';
import { PlaceSwatch, tintLabel, type TintName } from '../../features/places/components/PlaceSwatch';
import { categoryIcon } from '../../features/conversation/work/target';
import './foundations-specimen.css';

const tints: TintName[] = ['sand', 'sage', 'tide', 'iris', 'rose', 'graphite'];
const ramps = [
 ['canvas', 'Page ground'], ['frame', 'Place tint, window frame'], ['surface', 'Cards, tray, composer, sheets'],
 ['field', 'Chips, inputs, quiet buttons'], ['field-2', 'Hover / selected surface'], ['bubble', "Person’s messages and notes"],
 ['term', 'Terminal output, code blocks'], ['line', 'Hairlines'], ['guide', 'Work guide'], ['ink', 'Conversation rhythm'],
 ['ink-2', 'Work rhythm, secondary'], ['ink-3', 'Durations, directories, eyebrows'], ['accent', 'Primary action, running mark, links'],
 ['accent-soft', 'Selection'], ['accent-rule', 'Focus ring, update rule'], ['amber', 'Waiting on you · your call'],
 ['danger', 'Failed, irreversible, deletions'], ['success', 'Diff additions only. Done stays muted'],
];
const types = [
 ['title', 'Title', 'Tolerate trailing commas in config'], ['h1', 'Heading 1', 'What changed'], ['h2', 'Heading 2', 'Parser'],
 ['h3', 'Heading 3 · Head', 'Allow rm -rf build?'],
 ['prose', 'Prose', 'The tokenizer now skips a comma that sits right before a closing bracket. Strict mode still rejects it, so nothing downstream changes unless you opt in.'],
 ['label', 'Label', 'Read 3 files in internal/parse · Worked 42s · 4 steps'], ['caption', 'Caption', 'Update · generated · 1024×1024 · Suggested'],
 ['mono', 'Mono', '$ go test ./internal/parse/...'], ['mono-small', 'Mono small', '3.2s · 2m 14s · +42 −7'],
];
const spaces = ['space-1', 'space-2', 'space-3', 'space-4', 'space-5', 'space-6', 'space-8', 'space-12'] as const;
const rhythms = ['space-turn', 'space-answer', 'space-work-row', 'space-work-block', 'work-indent', 'chat-column-max-width', 'row-h', 'hit', 'panel'] as const;
const radii = ['chip', 'control', 'card', 'bubble', 'dock'] as const;
const marks: [Status, string][] = [['running', 'Running'], ['queued', 'Queued'], ['done', 'Done'], ['waiting', 'Your call'], ['failed', 'Failed'], ['stopped', 'Stopped'], ['paused', 'Paused'], ['incomplete', 'Incomplete']];
const categories = ['search', 'read', 'edit', 'create', 'run', 'test', 'browse', 'transfer', 'communicate', 'coordinate', 'plan', 'wait', 'work'];
const motion = [
 ['fast', 'Hover fills, hover actions fading in, Copy appearing, tooltip'],
 ['base', 'Folds and height, chevron rotation, tab switch, row enter, toast'],
 ['slow', 'Panel and sheet slide, place switch, lightbox'],
 ['spring', 'Decision tray rising, new bubble landing'],
] as const;
const materials = [
 ['solid', 'Opaque frame', 'Calm, but it feels like a web page. You lose the native sense that the window sits on your desktop.'],
 ['tinted', 'Tinted vibrancy · chosen', 'A blurred desktop under the place tint. The wallpaper reads as faint light, never as shapes.'],
 ['clear', 'Clear glass', 'Too much. The wallpaper fights the place tint and rail text loses contrast on busy wallpapers.'],
];
const foundation = design.foundation as Record<string, string>;
const value = (key: string) => foundation[key];

function Section({ number, title, children }: { number: string; title: string; children: ReactNode }) {
 return <section className="fd-section" aria-label={title}><div className="fd-heading"><span>{number}</span><h3>{title}</h3></div>{children}</section>;
}

function Rhythm({ work = false }: { work?: boolean }) {
 const facts = work
  ? ['Muted: --ink-2', 'Label 12 / 1.45 · mono 12', '4 between rows · 10 around a block', 'Indent 20 + 1px guide', 'Folded once settled · live while running']
  : ['Full: --ink', 'Prose 13 / 1.6', '32 between turns · 14 inside an answer', 'Column edge', 'Open'];
 return <div className="fd-card"><strong>{work ? 'Work rhythm' : 'Conversation rhythm'}</strong>
  {work ? <div className="fd-work"><div><Icon name="book" size="xs"/>Read 3 files in internal/parse <CodeText>0.4s</CodeText></div><div><Icon name="test" size="xs"/><CodeText>$ go test ./...</CodeText><CodeText>3.1s</CodeText></div></div>
   : <p className="fd-prose">Fixed. The parser now accepts trailing commas in arrays and objects, and the two failing fixtures pass.</p>}
  <dl className="fd-facts">{['Ink', 'Type', 'Space', 'Edge', 'Default'].map((label, i) => <div key={label}><dt>{label}</dt><dd>{facts[i]}</dd></div>)}</dl>
 </div>;
}

/** Fixtures stay in the Design system so none of these states can be mistaken for engine work. */
export function FoundationsSpecimen() {
 return <section className="foundations-specimen" aria-label="Foundations">
  <SectionHeading>Foundations</SectionHeading><p>Specimen · two rhythms, one place tint, native system type.</p>
  <Section number="01" title="Two rhythms"><div className="fd-grid"><Rhythm/><Rhythm work/></div></Section>
  <Section number="02" title="Place tints"><p>Children inherit their top-level place’s tint. Graphite is the neutral used by Now.</p>
   {(['light', 'dark'] as const).map(theme => <div key={theme} className="fd-tints" aria-label={`${theme} place tints`}>{tints.map(tint => <div key={tint} className="fd-tint" data-tint={tint} data-theme={theme}><div><PlaceSwatch tint={tint} role="rail"/><span/></div><strong>{tintLabel[tint]}{tint === 'tide' ? ' · default' : ''}</strong></div>)}</div>)}
  </Section>
  <Section number="03" title="Colour"><p>Each swatch shows light, then dark.</p><div className="fd-ramps">{ramps.map(([role, description]) => <div className="fd-ramp" key={role}><div className="fd-ramp-pair">{['light', 'dark'].map(theme => <span key={theme} data-theme={theme} data-role={role} role="img" aria-label={`${role} ${theme}`}/>)}</div><div><CodeText>--{role}</CodeText><p>{description}</p></div></div>)}</div></Section>
  <Section number="04" title="Type"><p>System UI (SF Pro · Segoe UI Variable) and SF Mono · Cascadia</p><div className="fd-type-table">{types.map(([role, label, sample]) => <div className="fd-type-row" key={role}><div><strong>{label}</strong><CodeText>{value(`type-${role}-size`) || value(`foundations-type-${role}-size`)} / {value(`type-${role}-leading`) || value(`foundations-type-${role}-leading`)} · {value(`type-${role}-weight`) || value(`foundations-type-${role}-weight`)}{role === 'label' ? ` · ${value('type-label-strong')}` : ''}{value(`type-${role}-tracking`) ? ` · ${value(`type-${role}-tracking`)}` : ''}{role === 'mono-small' ? ' · tabular' : ''}</CodeText></div><span data-type={role}>{sample}</span></div>)}</div></Section>
  <Section number="05" title="Space, radius, depth"><div className="fd-grid"><div className="fd-card"><strong>Scale · 4pt base</strong>{spaces.map(key => <div className="fd-space" key={key}><CodeText>{key}</CodeText><span data-space={key}/><CodeText>{value(key)}</CodeText></div>)}</div><div className="fd-card"><strong>Rhythm tokens</strong><dl className="fd-facts">{rhythms.map(key => <div key={key}><dt>{key}</dt><dd>{value(key)}</dd></div>)}</dl></div><div className="fd-card"><strong>Radius</strong><div className="fd-row">{radii.map(key => <div key={key}><div className="fd-radius" data-radius={key}/><CodeText>{value(`radius-${key}`)} {key}</CodeText></div>)}</div><strong>Depth</strong><div className="fd-row">{['1', '2', '3'].map((key, i) => <div key={key}><div className="fd-depth" data-depth={key}/><CodeText>sh-{key} {['rest', 'dock', 'sheet'][i]}</CodeText></div>)}</div></div></div></Section>
  <Section number="06" title="Status marks"><p>Still marks. Only the header’s “N running” dot breathes.</p><div className="fd-status-grid">{marks.map(([status, label]) => <div className="fd-mark" key={status}><StatusMark status={status} label={label}/><span>{label}</span>{status === 'running' && <CodeText>12s</CodeText>}</div>)}</div></Section>
  <Section number="07" title="Step categories"><p>Lucide · 13–14px in work rows, 16px in chrome</p><div className="fd-category-grid">{categories.map(category => <div className="fd-mark" key={category} data-category={category}><Icon name={categoryIcon(category)} size="sm"/><span>{category}</span></div>)}</div></Section>
  <Section number="08" title="Motion"><div className="fd-grid"><div className="fd-card fd-loop-card"><strong>Rules</strong><p>Motion explains a change. It never decorates. Only the live step shimmer and the header’s running dot loop. Rows, tabs and rail marks never pulse. Reduced motion makes both loops still.</p></div><div className="fd-card fd-loop-card"><strong>The two loops, live</strong><div className="fd-mark"><Shimmer active>Running the parser tests</Shimmer><CodeText>12s</CodeText></div><div className="fd-mark"><BreathingDot/>4 running</div><CodeText>Shimmer · {value('cf-shimmer-duration')} linear<br/>Breath · {value('breathe-duration')} ease-in-out</CodeText></div></div><div className="fd-motion-table" role="table" aria-label="Motion tokens">{motion.map(([key, description]) => <div className="fd-motion-row" role="row" key={key}><CodeText role="cell">{key}</CodeText><CodeText role="cell">{value(`dur-${key}`)} {key === 'fast' ? 'ease' : key === 'spring' ? value('spring') : value('chrome-ease')}</CodeText><span role="cell">{description}</span></div>)}</div></Section>
  <Section number="09" title="Materials"><p>Only the frame is translucent. Content is always opaque.</p><div className="fd-grid">{materials.map(([key, label, description]) => <div key={key} className="fd-material"><div className="fd-material-preview" data-material-option={key}><div className="fd-material-orb"/><div className="fd-material-window"><div className="fd-material-rail"><strong>Config parser</strong><span>Marketing · codeaf</span><span>Q3 report</span></div><div className="fd-material-content"/></div></div><strong>{label}</strong><p>{description}</p></div>)}</div><p>The frame alone uses vibrancy. Cards, the composer, the tray, popovers and sheets stay opaque. Inactive windows, reduced transparency and increased contrast use solid frame. Never stack two blurs.</p></Section>
 </section>;
}
