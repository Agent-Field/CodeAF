import { createRoot } from 'react-dom/client';
import '../../src/App.css';
import { BreathingDot, Shimmer } from '../../src/components/ui';

createRoot(document.getElementById('root')!).render(<main>
 <p><Shimmer active>Running the parser tests</Shimmer></p>
 <p><Shimmer>Settled step</Shimmer></p>
 <p><BreathingDot /><span>4 running</span></p>
</main>);
