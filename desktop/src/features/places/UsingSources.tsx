import { Icon, IconButton, Tag } from '../../components/ui';
import type { IconName } from '../../components/ui';
import { handoffFor, placeList, provenance, sourceDetail, sourceName } from './using-model';
import type { SourceHandoff, UsedSource, UsingBundle } from './using-types';

const icons: Record<UsedSource['kind'], IconName> = { folder: 'folder', repo: 'branch', file: 'file', url: 'web', chat: 'tab' };

type RowProps = {
  source: UsedSource;
  bundle: UsingBundle;
  /** How this row is not given as asked: 'left-out' (over the source limit) or 'refused'. Absent for a source the model reads. */
  held?: 'left-out' | 'refused';
  onOpen?: (handoff: SourceHandoff) => void;
};

/** Who put the source here: the place or places it was filed in, and the AI when it was the AI that filed it. */
function Origin({ source, bundle }: { source: UsedSource; bundle: UsingBundle }) {
  const rows = provenance(source, bundle);
  const names = placeList(rows.map(row => row.place));
  const byAi = rows.some(row => row.by === 'ai');
  return <span className="using-row-aside">{names}{byAi ? ' · added by the AI' : ''}</span>;
}

function SourceRow({ source, bundle, held, onOpen }: RowProps) {
  const name = sourceName(source);
  const detail = sourceDetail(source);
  const missing = source.status === 'missing';
  const handoff = handoffFor(source);
  const note = held === 'refused' ? source.reason : missing ? source.reason ?? 'Not found where it was added.' : undefined;
  return (
    <li className="using-row" data-kind="source" data-status={held ?? source.status} data-source-key={source.key}>
      <div className="using-row-main using-source-main">
        <Icon name={missing ? 'fileMissing' : held === 'refused' ? 'ban' : icons[source.kind]} size="sm" />
        <span className="using-source-text">
          <span className="using-source-name">{name}</span>
          {detail && detail !== name && <span className="using-source-detail">{detail}</span>}
          {note && <span className="using-source-note">{note}</span>}
        </span>
        <Origin source={source} bundle={bundle} />
      </div>
      {missing && <Tag tone="neutral" className="using-row-tag">Not found</Tag>}
      {held === 'left-out' && <Tag tone="neutral" className="using-row-tag">Left out</Tag>}
      {held === 'refused' && <Tag tone="danger" className="using-row-tag">Refused</Tag>}
      {onOpen && handoff && <IconButton size="row" icon="external" iconSize="sm" label={`Open ${name}`} className="using-row-open" onClick={() => onOpen(handoff)} />}
    </li>
  );
}

/**
 * The sources, in three honest groups: what the model reads, what was left out because the conversation is over its source
 * limit (named, never given), and what the source policy refused (named with the engine's reason). A source the model reads
 * but whose file has gone says so in place; it is not removed from the list.
 */
export function SourceList({ bundle, onOpen }: { bundle: UsingBundle; onOpen?: (handoff: SourceHandoff) => void }) {
  return (
    <>
      {bundle.sources.length > 0 && (
        <ul className="using-list" aria-label="Sources the model reads">
          {bundle.sources.map(source => <SourceRow key={source.key} source={source} bundle={bundle} onOpen={onOpen} />)}
        </ul>
      )}
      {bundle.trimmed.length > 0 && (
        <>
          <h4 className="using-sublabel">Left out for room</h4>
          <ul className="using-list" aria-label="Sources left out for room">
            {bundle.trimmed.map(source => <SourceRow key={source.key} source={source} bundle={bundle} held="left-out" onOpen={onOpen} />)}
          </ul>
        </>
      )}
      {bundle.refused.length > 0 && (
        <>
          <h4 className="using-sublabel">Refused</h4>
          <ul className="using-list" aria-label="Sources refused">
            {bundle.refused.map(source => <SourceRow key={source.key} source={source} bundle={bundle} held="refused" />)}
          </ul>
        </>
      )}
    </>
  );
}
