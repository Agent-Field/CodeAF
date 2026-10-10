import type { ReactElement } from 'react';
import { DropdownMenu, type MenuEntry } from '../../../components/ui';
import './switcher.css';

/**
 * The place menu hung under the Home tab while the rail is put away (Places 9c, Components
 * "Place switcher"). The shared menu already walks with Up, Down, Enter and Escape; this
 * only pins the measured column and lines its start edge up with the tab. Inbox is not a
 * row: Iteration 2 counts questions elsewhere on the frame pill.
 */
export function Switcher({ items, children, onOpenChange }: { items: readonly MenuEntry[]; children: ReactElement; onOpenChange?: (open: boolean) => void }) {
  return <DropdownMenu className="place-switcher" label="Place switcher" align="start" items={items} onOpenChange={onOpenChange}>{children}</DropdownMenu>;
}
