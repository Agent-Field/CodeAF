# Find text in a desktop web page

## How do I find text in a desktop web page with Command F or Control F

When native page search is available, Command F on macOS or Control F on Linux replaces the web tab’s address with Find in page. Type text to search. Enter selects the next match; Shift Enter selects the previous match. Escape closes the field and restores the address.

The native find command is a separate dependency. Until it is installed, the address stays visible and no find field appears. Browser development mode cannot search a native page.

## Why does web page find show no count or no n of m

The page’s public native search API does not report the current match number. codeaf never invents “n of m”. When the platform supplies a positive total, the field shows “N matches”. Unknown totals and failed requests show no count. Find highlights belong to the web page’s native selection.
