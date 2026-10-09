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
      iconSize="xs"
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
    <li className="attachment attachment-file">
      <Icon name="file" size="sm" />
      <Text className="attachment-name">{props.item.file.name}</Text>
      <Text className="attachment-size">{formatSize(props.item.file.size)}</Text>
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
