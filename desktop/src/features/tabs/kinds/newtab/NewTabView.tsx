import type { ReactNode } from 'react';
import { Icon } from '../../../../components/ui';
import { splitMatch, type NewTabRow, type NewTabSection } from './rows';
import './newtab.css';

function RowLabel({ row, query }: { row: NewTabRow; query: string }) {
  const [before, match, after] = row.kind === 'ask' || row.kind === 'web' ? [row.label, '', ''] : splitMatch(row.label, query);
  return <span className="newtab-row-label">{before}{match && <b>{match}</b>}{after}{row.detail && <>{' '}<span className="newtab-row-detail">{row.detail}</span></>}</span>;
}

export type NewTabViewProps = {
  /** The id the list and its rows hang their ids from. */
  id: string;
  /** The text control: the live input in the pane, a plain span in the specimen. */
  field: ReactNode;
  query: string;
  sections: NewTabSection[];
  activeRowId?: string;
  caption: string;
  /** What Enter does for the highlighted row; the conversation row by default. */
  enterHint?: string;
  /** Live only: pointer and click wiring. A specimen leaves it out and draws a still card. */
  onHover?: (row: NewTabRow) => void;
  onPick?: (row: NewTabRow) => void;
};

/** The drawing of the new tab (design 3f / Components "Command field"): shared by the live pane and the Design system specimen. */
export function NewTabView({ id, field, query, sections, activeRowId, caption, enterHint = '↵ to start a conversation', onHover, onPick }: NewTabViewProps) {
  return <div className="newtab" data-scroll-key="newtab">
    <div className="newtab-field">
      <div className="newtab-input-row">
        <Icon name="search"/>
        {field}
        <span className="newtab-hint">{enterHint}</span>
      </div>
      <div className="newtab-rule"/>
      <div className="newtab-list" id={`newtab-${id}`} role="listbox" aria-label="Suggestions">
        {sections.map((section, at) => <div key={section.title ?? `section-${at}`} role="group" aria-label={section.title}>
          {section.title && <span className="newtab-section" aria-hidden="true">{section.title}</span>}
          {section.rows.map(row => <div key={row.id} id={`newtab-${id}-${row.id}`} className="newtab-row" role="option" aria-selected={row.id === activeRowId}
            onMouseMove={onHover && (() => onHover(row))} onMouseDown={onPick && (event => event.preventDefault())} onClick={onPick && (() => onPick(row))}>
            {row.dot ? <span className="tab-dot" aria-hidden="true"/> : <Icon name={row.icon} size="sm"/>}
            <RowLabel row={row} query={query}/>
            {row.hint && <span className="newtab-row-hint">{row.hint}</span>}
          </div>)}
        </div>)}
      </div>
    </div>
    <p className="newtab-caption">{caption}</p>
  </div>;
}
