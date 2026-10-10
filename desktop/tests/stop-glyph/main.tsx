import { createRoot } from 'react-dom/client';
import '../../src/App.css';
import { ThemeProvider } from '../../src/design/ThemeProvider';
import { Icon, StopGlyph } from '../../src/components/ui';
import './harness.css';

createRoot(document.getElementById('root')!).render(<ThemeProvider><main>
 <div className="stop-row stop-tone-ink" data-tone="ink">
  <StopGlyph size="md"/>
  <Icon name="send" size="md"/>
 </div>
 <div className="stop-row stop-tone-accent" data-tone="accent">
  <StopGlyph size="sm"/>
  <Icon name="send" size="sm"/>
 </div>
</main></ThemeProvider>);
