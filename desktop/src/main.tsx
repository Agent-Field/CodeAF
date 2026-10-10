import './design/inputModality';
import { ThemeProvider } from './design/ThemeProvider';
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { BannerSpecimen } from "./features/nextup/Banner";
import { FramePillSpecimen } from "./features/nextup/FramePill";
import { QueuePopoverSpecimen } from "./features/nextup/QueuePopover";
import { FilterTabsSpecimen } from "./components/ui/FilterTabs";
import { SelectSpecimen } from "./components/ui/Select";
import { connectAttentionNotifications } from "./lib/native/notify";

const stopNotifications = connectAttentionNotifications();
import.meta.hot?.dispose(stopNotifications);

// The banner is measured on its own page so the shell's other live regions are
// not in the way. The product never navigates here; the strip mounts Banner.
const specimen = new URLSearchParams(window.location.search).get('specimen');
const page = specimen === 'nextup-banner' ? <BannerSpecimen/> : specimen === 'frame-pill' ? <FramePillSpecimen/> : specimen === 'queue-popover' ? <QueuePopoverSpecimen/> : specimen === 'filter-tabs' ? <FilterTabsSpecimen/> : specimen === 'select' ? <SelectSpecimen/> : <App/>;

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <ThemeProvider>{page}</ThemeProvider>
  </React.StrictMode>,
);
