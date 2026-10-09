import { useEffect, useState } from 'react';
import { Text } from '../../../components/ui';
import { readSettingsSummary, SettingsPage } from '../../settings';
import type { PaneRenderProps, PreviewRenderProps } from './slots';

/** The Settings tab's body: the Models page, scrolling inside the card. */
export function SettingsPane(_: PaneRenderProps) {
  return <div className="settings-scroll"><SettingsPage/></div>;
}

/** The hover card and overview card text: which models are pinned, read from the engine (nothing while it cannot say). */
export function SettingsPreview(_: PreviewRenderProps) {
  const [summary, setSummary] = useState('');
  useEffect(() => { let live = true; void readSettingsSummary().then(text => { if (live) setSummary(text); }); return () => { live = false; }; }, []);
  return summary ? <Text>{summary}</Text> : null;
}
