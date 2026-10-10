import { createRoot } from 'react-dom/client';
import '../../src/App.css';
import { Tag } from '../../src/components/ui/Chip';

createRoot(document.getElementById('root')!).render(<Tag tone="plain">Suggested</Tag>);
