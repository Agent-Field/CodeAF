# Web tab: open design questions

Questions the web lane met while building the native web tab (design Shell 3d,
3j, 3k; Components "Link chip"; Interactions). Each one ships with the
conservative assumption stated, so nothing waits on an answer. These will be
consolidated into the design ledger.

| # | Question | Assumption shipped |
|---|---|---|
| W1 | Every app surface over a page (menu, hover card, tooltip, dialog, palette, overview, toast) is drawn under a native view, so the view hides while one overlaps its sheet. Tooltips of the web header's own buttons drop into the sheet, so hovering Back for 500ms hides the page until the tooltip goes. Should the header's tooltips open above, or should the page stay and the tooltip be dropped? | The rule is uniform: anything overlapping hides the page, and the last picture of the page stands in while it is hidden. No tooltip is suppressed. |
| W2 | The tab glyph's monogram is the first letter of the tab title (`Tab.tsx` `KindIcon`), which for a web tab becomes the page title ("j" for "json package"). The design's address field and tab both show the site's letter. | The address field uses the site's letter. The tab keeps the shared rule until the tab primitive's owner decides; a one-line patch (pass the site as the monogram source for `web`) is in the lane report. |
| W3 | Favicons. Design 3j: "the real favicon if the engine fetched the site, else a monogram". A web tab's own page has a favicon the view knows, but Tauri exposes none and the engine never fetched the site. | Monogram only. No favicon is fetched by the renderer or the native side. |
| W4 | Load progress. The 2px line can show a fraction. WebKitGTK reports `estimated-load-progress`; WKWebView only through KVO. | The line runs as the indeterminate sweep while loading, on both platforms. |
| W5 | A refused download, popup or permission request needs one muted line. The sheet is under the native view, so a line there is invisible. | The line replaces the dimmed path inside the address field for one state update (`role="status"`). |
| W6 | Where does "Start a conversation with this page" put the page? | A new conversation tab opens with an unsent draft naming the page's title and address, and the native snapshot of the page offered to that tab's composer as `page.png`. Nothing is sent and no model is called until the person sends. The button is absent when the workspace has not registered a host. |
| W7 | Cookies and logins. Should web tabs share one persistent store, be private, or follow a place? | One persistent store for all web tabs, apart from the app's own (`web-tabs` data directory on Linux; a fixed data-store identifier on macOS 14+, the default store before 14). |
| W8 | Keyboard: ⌘L / Ctrl+L for the address, ⌘R reload, ⌘[ ⌘] history. While the page has focus the keys go to the native view, never to the app. | Not bound yet; the controls are reachable by Tab and Enter/Space. Shortcut labels belong in `design/keyboard.ts` (rail and keys lane). |
| W9 | Find in page. | Absent: neither platform's find is exposed by Tauri 2.12. |
| W10 | A page's `window.open` or `target=_blank`. | A typed `web://new-tab` request: the workspace opens a web tab in front; without a workspace host the link goes to the default browser, as a link chip's click does. A page never gets a window. |
| W11 | Hover and overview cards for web tabs are the one kind with a picture (3k). The hover-preview and overview hosts do not consume a kind's `preview` slot yet. | `WebPreview` fills the web kind's slot: the native snapshot when there is one, else a line saying why there is none. Wiring the slot into `previewHost`/`TabOverview` is the preview lane's; a patch is in the lane report. |
