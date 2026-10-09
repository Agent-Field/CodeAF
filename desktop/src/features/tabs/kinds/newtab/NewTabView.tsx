import type { ReactNode } from 'react';
import { Icon } from '../../../../components/ui';
import { splitMatch, type NewTabRow, type NewTabSection } from './rows';
import './newtab.css';

function RowLabel({ row, query }: { row: NewTabRow; query: string }) {
  const [before, match, after] = row.kind === 'ask' ? [row.label, '', ''] : splitMatch(row.label, query);
  return <span className="newtab-row-label">{before}{match && <b>{match}</b>}{after}{row.detail && <>{' '}<span className="newtab-row-detail">{row.detail}</span></>}</span>;
}

/** A history row shows its ↵ only while it is the chosen one, as the design does; every other row's hint is fixed. */
const hintOf = (row: NewTabRow, activeRowId?: string) => row.hint ?? (row.kind === 'history' && row.id === activeRowId ? '↵' : undefined);

export type NewTabViewProps = {
  /** The id the list and its rows hang their ids from. */
  id: string;
  /** The text control: the live input in the pane, a plain span in the specimen. */
  field: ReactNode;
  query: string;
  sections: NewTabSection[];
  activeRowId?: string;
  caption: string;
  /** Live only: pointer and click wiring. A specimen leaves it out and draws a still card. */
  onHover?: (row: NewTabRow) => void;
  /** `background` is the ⌘ (Ctrl) click: open without leaving the field. */
  onPick?: (row: NewTabRow, press: { background: boolean }) => void;
};

/** The drawing of the new tab (design 3f / Components "Command field"): shared by the live pane and the Design system specimen. */
export function NewTabView({ id, field, query, sections, activeRowId, caption, onHover, onPick }: NewTabViewProps) {
  return <div className="newtab">
    <div className="newtab-field">
      <div className="newtab-input-row">
        <Icon name="search"/>
        {field}
        <span className="newtab-hint">↵ to start a conversation</span>
      </div>
      <div className="newtab-rule"/>
      <div className="newtab-list" id={`newtab-${id}`} role="listbox" aria-label="Suggestions">
        {sections.map((section, at) => <div key={section.title ?? `section-${at}`} role="group" aria-label={section.title}>
          {section.title && <span className="newtab-section" aria-hidden="true">{section.title}</span>}
          {section.rows.map(row => <div key={row.id} id={`newtab-${id}-${row.id}`} className="newtab-row" role="option" aria-selected={row.id === activeRowId}
            onMouseMove={onHover && (() => onHover(row))} onMouseDown={onPick && (event => event.preventDefault())} onClick={onPick && (event => onPick(row, { background: event.metaKey || event.ctrlKey }))}>
            {row.dot ? <span className="tab-dot" aria-hidden="true"/> : <Icon name={row.icon} size="sm"/>}
            <RowLabel row={row} query={query}/>
            {hintOf(row, activeRowId) && <span className="newtab-row-hint">{hintOf(row, activeRowId)}</span>}
          </div>)}
        </div>)}
      </div>
    </div>
    <p className="newtab-caption">{caption}</p>
  </div>;
}
