import type { HTMLAttributes } from 'react';
import { DropdownMenu, HomeTitle, IconButton, type MenuEntry } from '../../../components/ui';
import { PlaceTitleField } from '../home/PlaceTitle';
import { PlaceBreadcrumb, type Crumb } from '../home/PlaceBreadcrumb';
import { PlaceSwatch, type TintName } from './PlaceSwatch';
import './places-components.css';

export type { Crumb };

type PlaceHeadingProps = Omit<HTMLAttributes<HTMLElement>, 'title'> & {
  title: string;
  /** The effective tint (inherited or its own). */
  tint: TintName;
  /** Ancestors, outermost first ("All places"). Empty for the root, which draws no breadcrumb at all. */
  breadcrumb?: readonly Crumb[];
  /** The ⋯ menu. Without entries there is no button: a menu with nothing in it is not drawn. */
  menu?: readonly MenuEntry[];
  menuLabel?: string;
  /** Inline rename (Q-14): the title becomes a field holding the current name. Enter commits, Escape cancels. */
  renaming?: boolean;
  onRename?: (name: string) => void;
  onRenameCancel?: () => void;
};

/** The anatomy every place page, History and All places share (Components "Places · page"): a breadcrumb, then the tint square,
 * the 28px title and the ⋯ menu. Sections follow under their own 11px labels, outside this component. */
export function PlaceHeading({ title, tint, breadcrumb = [], menu, menuLabel, renaming, onRename, onRenameCancel, className = '', ...props }: PlaceHeadingProps) {
  return <header {...props} className={`places-heading ${className}`}>
    <PlaceBreadcrumb crumbs={breadcrumb}/>
    <div className="places-heading-row">
      <PlaceSwatch tint={tint} role="title"/>
      {renaming ? <PlaceTitleField title={title} onRename={onRename} onCancel={onRenameCancel}/> : <HomeTitle className="places-heading-title">{title}</HomeTitle>}
      {!renaming && menu && menu.length > 0 && <DropdownMenu items={menu} label={menuLabel ?? `${title} actions`}>
        <IconButton className="places-heading-menu" label="Place actions" icon="more" iconSize="sm"/>
      </DropdownMenu>}
    </div>
  </header>;
}
