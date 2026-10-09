// Test-only entry for the Places Home: it mounts one specimen scenario (?scenario=place|empty|root|first|now|loading|error|offline, &bare=1 for no verbs)
// so the browser tests never depend on the app shell, which owns wiring later. Not part of the production build.
import '../../../src/design/inputModality';
import '../../../src/App.css';
import React from 'react';
import ReactDOM from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { HomeSpecimen, homeScenarios, type HomeScenario } from '../../../src/features/places/specimens/HomeSpecimen';

const params = new URLSearchParams(location.search);
const requested = params.get('scenario') ?? 'place';
const scenario = (requested in homeScenarios ? requested : 'place') as HomeScenario;
ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <ThemeProvider><main className="places-harness"><HomeSpecimen key={`${scenario}${params.get('bare')}`} scenario={scenario} withoutVerbs={params.get('bare') === '1'}/></main></ThemeProvider>
  </React.StrictMode>,
);
