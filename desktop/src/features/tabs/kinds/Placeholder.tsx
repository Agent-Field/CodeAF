import type { ComponentType } from 'react';
import { Icon, Text, type IconName } from '../../../components/ui';
import type { PaneRenderProps } from './slots';
import './placeholder.css';

/**
 * The pane of a kind with no backing yet. Nothing in the live app opens these kinds; the lane that
 * builds the kind replaces its `pane` with the real renderer in that kind's own file.
 */
export function placeholderPane(label: string, icon: IconName): ComponentType<PaneRenderProps> {
  return function PlaceholderPane() {
    return <div className="kind-placeholder"><Icon name={icon} size="lg"/><Text>{label} is not available yet.</Text></div>;
  };
}
