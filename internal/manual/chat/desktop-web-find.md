# Find text in a desktop web page

## How do I find text in a desktop web page with Command F or Control F

Command F on macOS or Control F on Linux, while a web tab is focused, replaces the address with Find in page. Type text to search. Enter selects the next match; Shift Enter selects the previous match. Escape closes the field and restores the address.

The desktop app searches the page itself. The page is not given a way to call back into the app. A browser window running this interface has no native page, so the address stays put there.

## Why does web page find show no count or no n of m

The page’s public native search API does not report the current match number. codeaf never invents “n of m”. When the platform supplies a positive total, the field shows “N matches”. Unknown totals and failed requests show no count. Find highlights belong to the web page’s native selection.
