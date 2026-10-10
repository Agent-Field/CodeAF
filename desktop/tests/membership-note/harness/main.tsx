// This isolated fixture replays supplied records through the production note and Places client.
import '../../../src/design/inputModality';
import '../../../src/App.css';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { ToastRegion } from '../../../src/components/ui';
import { PlacesShellProvider, usePlacesShellController } from '../../../src/features/places/shell/PlacesShell';
import { MembershipNote, type MembershipNoteProps } from '../../../src/features/places/using/MembershipNote';

function Fixture() {
  const shell = usePlacesShellController();
  const notes = JSON.parse(sessionStorage.getItem('membership-note-fixture') ?? '[]') as MembershipNoteProps[];
  return <PlacesShellProvider value={shell}><main>{notes.map((note, i) => <MembershipNote key={i} {...note} />)}<ToastRegion /></main></PlacesShellProvider>;
}
createRoot(document.getElementById('root')!).render(<ThemeProvider><Fixture /></ThemeProvider>);
