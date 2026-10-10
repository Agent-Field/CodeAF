# Desktop Home layout

## How does the desktop Home fit a narrow window?

The desktop Home uses a centered 680px column on wide windows. At window widths
of 850px or less, Home uses the available pane width with 16px side gutters.
The breadcrumb and title stay above the sections. A place title has its tint
swatch and a Place actions menu when actions are available.

## Why does the desktop Home composer stay at the bottom when I scroll?

The desktop Home composer stays below the scrolling sections, aligned with the
same column on populated places, empty places, Now and All places. The bottom
of the scrolling area fades into the page. Scrolling the sections does not move
the composer. If the host supplies no composer, Home draws no composer slot.

## Does desktop Home show skeleton sections while loading?

Desktop Home draws sections when their data arrives. Unknown section data draws
nothing; there are no skeleton sections. During the first read, Home may show
“Loading places”. Available sections remain visible while other reads finish.
