import type { ReactNode } from 'react';
import { HomeTitle } from '../../../components/ui';
import { PlaceTile } from '../components/PlaceTile';
import { HomePlacesSection, type DragState, type Runner } from '../HomeSections';
import type { PlaceActions } from '../place-actions';

/** Places 8e keeps the explainer with the title and offers two ordinary tiles, without a tour. */
export function FirstLaunch({ actions, readOnly, runner, drag, notices }: {
  actions: PlaceActions; readOnly?: boolean; runner: Runner; drag: DragState; notices: ReactNode;
}) {
  return <>
    <header className="home-heading all-places-heading root-home-heading first-launch-heading" aria-label="First launch">
      <HomeTitle>All places</HomeTitle>
      <p className="home-empty-sentence">Places hold work that belongs together, with what the AI should know about it. Open a folder or repo to make one, or just name one.</p>
    </header>
    {notices}
    <HomePlacesSection label="Places" places={[]} siblings={[]} actions={actions} readOnly={readOnly}
      runner={runner} drag={drag} showLabel={false} newLabel="Name a place"
      extraTiles={actions.openFolderAsPlace && <PlaceTile mode="new" icon="folderOpen" label="Open a folder or repo"
        disabled={readOnly || runner.busy} onCreate={() => actions.openFolderAsPlace?.()}/>}/>
  </>;
}
