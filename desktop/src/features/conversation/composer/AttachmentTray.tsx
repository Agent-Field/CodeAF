import { Icon, IconButton, Text } from '../../../components/ui';
import { formatSize, type Attachment } from './attachments';
import './attachment-tray.css';

type Remove = (id: string) => void;
type RowProps = { item: Attachment; onRemove: Remove };

function RemoveButton({ item, onRemove }: RowProps) {
  return (
    <IconButton
      className="attachment-remove"
      label={`Remove ${item.file.name}`}
      icon="close"
      iconSize={item.picture ? "xs" : "micro"}
      onClick={() => onRemove(item.id)}
    />
  );
}

function Picture(props: RowProps) {
  return (
    <li className="attachment attachment-picture">
      <img className="attachment-thumb" src={props.item.previewUrl} alt={props.item.file.name} />
      <RemoveButton {...props} />
    </li>
  );
}

function FileChip(props: RowProps) {
  return (
    <li className="attachment attachment-file" title={`${props.item.file.name} · ${formatSize(props.item.file.size)}`} aria-label={`${props.item.file.name}, ${formatSize(props.item.file.size)}`}>
      <Icon name="file" size="tiny" />
      <Text tone="default" className="attachment-name">{props.item.file.name}</Text>
      <RemoveButton {...props} />
    </li>
  );
}

/** Pictures as thumbnails, other files as name + size chips. Renders nothing when empty. */
export function AttachmentTray({ items, onRemove }: { items: Attachment[]; onRemove: Remove }) {
  if (items.length === 0) return null;
  return (
    <ul className="attachment-tray" aria-label="Attachments">
      {items.map(item =>
        item.picture ? (
          <Picture key={item.id} item={item} onRemove={onRemove} />
        ) : (
          <FileChip key={item.id} item={item} onRemove={onRemove} />
        ),
      )}
    </ul>
  );
}
