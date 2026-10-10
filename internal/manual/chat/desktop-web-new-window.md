# A page opening a new window in the desktop app

## What happens when a page opens a new window or a target=_blank link

In the desktop app, a page that calls window.open, or a link with target=_blank,
does not get a window of its own. codeaf adds a web tab in the background,
immediately after the tab that asked. The tab you are reading stays in front.
Only an http or https address is opened. A javascript, file, or data address
is dropped and nothing is said.

## Does target=_blank open another window in the desktop app

No. target=_blank and window.open open a background web tab after the tab that
held the page. codeaf does not create a native window for that request, and it
does not send the address to your browser.

## Why a page opened only five new tabs

One page can open at most five of these tabs in any two seconds. Further
requests in that time are dropped, so a page cannot fill the strip. After two
seconds it can open more. Each page has its own count.
