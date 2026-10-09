import { Button, DropdownMenu, Icon, type MenuEntry } from '../../../components/ui';
import './model-picker.css';

export type ModelOption = {
  id: string;
  label: string;
  /** Digit that switches here directly; set only when the engine can honour a swap. */
  shortcut?: string;
};

export type ModelPickerProps = {
  /** Only models the engine can run; today that is one. */
  models: readonly ModelOption[];
  selectedId: string;
  /** Absent when the engine exposes no routing, so choosing the checked entry is a no-op. */
  onSelect?: (id: string) => void;
};

/**
 * The composer's model label and its quick-swap popover (design 1e). The designed
 * segments, Effort row, pinned shortcuts and "All models…" need engine routing the
 * engine does not expose, so they are absent until `models` carries more than one entry.
 */
export function ModelPicker({ models, selectedId, onSelect }: ModelPickerProps) {
  const selected = models.find(model => model.id === selectedId);
  if (!selected) return null;
  const items: MenuEntry[] = models.map(model => ({
    id: model.id,
    label: model.label,
    shortcut: model.shortcut ? `⌘${model.shortcut}` : undefined,
    checked: model.id === selectedId,
    onSelect: () => { if (model.id !== selectedId) onSelect?.(model.id); },
  }));
  return (
    <DropdownMenu label="Model" items={items}>
      <Button className="model-picker" aria-label={`Model: ${selected.label}`}>
        <span className="model-picker-label">{selected.label}</span>
        <Icon name="chevron" size="xs" />
      </Button>
    </DropdownMenu>
  );
}
