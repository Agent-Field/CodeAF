import { Button, Text } from '../../components/ui';
import type { WebErrorKind } from './model';
import { webErrorLine } from './strings';

type Props = {
  kind: WebErrorKind;
  onExternal: () => void;
  onReload: () => void;
};

/** The sheet names a failed load quietly; the tab owns its failed glyph. */
export function WebStatus({ kind, onExternal, onReload }: Props) {
  if (kind === 'none') return null;
  return <div className="web-state web-status" role="alert">
    <Text className="web-status-line">{webErrorLine[kind]}</Text>
    <div className="web-state-actions">
      <Button variant="quiet" onClick={onExternal}>Open in browser</Button>
      <Button variant="quiet" onClick={onReload}>Reload</Button>
    </div>
  </div>;
}
