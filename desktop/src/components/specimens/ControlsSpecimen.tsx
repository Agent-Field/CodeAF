import { useState } from 'react';
import design from '../../design/tokens.json';
import { Button, Chip, IconButton, Icon, Row, RowActions, SectionHeading, Segmented, StatusMark, Surface, Tag, Text, TextInput, TruncatedText, type Status } from '../ui';

type Tint = keyof typeof design.tints.hues;
const tints = Object.keys(design.tints.hues) as Tint[];
const roles = ['canvas', 'frame', 'surface', 'field', 'field-2', 'bubble', 'term', 'line', 'ink', 'ink-2', 'ink-3', 'accent', 'accent-soft', 'amber', 'danger', 'success'] as const;
const marks: { status: Status; label: string }[] = [
 { status: 'running', label: 'Running' }, { status: 'queued', label: 'Queued' }, { status: 'done', label: 'Done' }, { status: 'waiting', label: 'Your call' },
 { status: 'failed', label: 'Failed' }, { status: 'stopped', label: 'Stopped' }, { status: 'paused', label: 'Paused' }, { status: 'incomplete', label: 'Incomplete' },
];
const noop = () => {};

/** Design v3 foundations and shared controls with every state, inside one tinted space. Specimen only. */
export function ControlsSpecimen() {
 const [tint, setTint] = useState<Tint>(design.tints.default as Tint);
 const [choice, setChoice] = useState('either');
 return <Surface direction="column">
  <SectionHeading>Foundations and controls</SectionHeading>
  <Text>Specimen. 28px controls, radius 8, 12px medium labels. One primary per surface; the safe choice is the quiet one.</Text>
  <Segmented label="Space tint" options={tints.map(value => ({ value, label: value[0].toUpperCase() + value.slice(1) }))} value={tint} onChange={setTint}/>
  <div className="controls-specimen" data-tint={tint}>
   <div className="controls-specimen-group"><span className="controls-specimen-label">Colour roles</span>
    <div className="role-swatches">{roles.map(role => <div key={role} className="role-swatch"><span className="role-swatch-chip" data-role={role}/><small>{role}</small></div>)}</div>
   </div>
   <div className="controls-specimen-group"><span className="controls-specimen-label">Buttons</span>
    <div className="controls-specimen-row">
     <Button variant="primary">Allow once</Button><Button variant="raised">Always…</Button><Button variant="quiet">Deny</Button><Button variant="ghost">Later</Button><Button variant="danger">Stop task</Button><Button variant="quiet" disabled>Disabled</Button>
    </div>
    <div className="controls-specimen-row controls-specimen-icons">
     <IconButton label="Copy" icon="copy" iconSize="sm"/><IconButton label="Edit" icon="pencil" iconSize="sm"/><IconButton label="Attach" icon="attach" iconSize="sm"/><IconButton label="Close" icon="close" iconSize="sm"/>
     <span className="controls-specimen-note">Icon buttons · hover for the tooltip</span>
    </div>
   </div>
   <div className="controls-specimen-columns">
    <div className="controls-specimen-group"><span className="controls-specimen-label">Segmented</span>
     <Segmented label="Choice specimen" options={[{ value: 'a', label: 'A' }, { value: 'either', label: 'Either' }, { value: 'b', label: 'B' }]} value={choice} onChange={setChoice}/>
    </div>
    <div className="controls-specimen-group controls-specimen-grow"><span className="controls-specimen-label">Field · rest, focus</span>
     <TextInput appearance="field" aria-label="Field specimen" placeholder="Say why (optional)"/>
    </div>
    <div className="controls-specimen-group"><span className="controls-specimen-label">Tags</span>
     <div className="controls-specimen-row"><Tag tone="plain">Suggested</Tag><Tag>Reversible</Tag><Tag tone="danger">Irreversible</Tag><Tag tone="key">⌘↵</Tag></div>
    </div>
   </div>
   <div className="controls-specimen-group"><span className="controls-specimen-label">Chips</span>
    <div className="controls-specimen-row"><Chip><Icon name="fileCode" size="xs"/>lexer.go</Chip><Chip muted><Icon name="fileMissing" size="xs"/>old_lexer.go</Chip></div>
   </div>
   <div className="controls-specimen-group"><span className="controls-specimen-label">Status marks</span>
    <div className="controls-specimen-row">{marks.map(mark => <span key={mark.status} className="controls-specimen-mark"><StatusMark status={mark.status} label={mark.label}/>{mark.label}</span>)}</div>
   </div>
   <div className="controls-specimen-group"><span className="controls-specimen-label">Rows · hover, selected, row actions</span>
    <Row><StatusMark status="running" label="Running"/><TruncatedText text="Update fixtures"/></Row>
    <Row selected><StatusMark status="waiting" label="Your call"/><TruncatedText text="Port fix to v1 branch"/>
     <RowActions><IconButton size="row" iconSize="xs" label="Open" icon="arrowUpRight" onClick={noop}/><IconButton size="row" iconSize="xs" label="Pause" icon="pause" onClick={noop}/><IconButton size="row" iconSize="xs" label="More" icon="more" onClick={noop}/></RowActions>
    </Row>
   </div>
  </div>
 </Surface>;
}
