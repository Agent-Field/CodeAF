# Docs, Sheets, Slides and Canvas

## First-party apps and where to open them

Open the built-in editors or find templates in the Kit

Docs, Sheets, Slides and Canvas are apps with their own record types, screens and Actions. They appear in Apps on a new profile. In Desk's Apps place, open Kit to find the codeaf publisher templates. A package uses sample records in the preview. New documents and edits in the kept app go to your records.

Docs displays Markdown. Sheets supports cell edits, paste, range clear and fill, arithmetic and SUM over ranges. Each worksheet takes up to 500 rows and 32 columns. Slides opens and presents individual slides. Canvas opens a board with movable text. Existing canvas files still open in Canvases with their original assets, versions and marks. New app from Canvases opens the same Builder as New app from Apps.

## Fork an editor and ask for a change

Choose Fork… from the app menu in Desk, review the package card, then choose Fork. The new app has its own definition history. Its document type is shared with the original app, so the same documents remain available. Open Edit app from the app menu and describe what should change. Review and apply the Builder's proposal before using the new controls.

Choose Check for update for a codeaf package. The update card compares the saved publisher version, your version and the new package. For each conflict choose Keep mine or Use publisher’s, then Check choices. Apply keeps the resulting definition. Local changes that do not conflict carry forward.

## Edit documents with codeaf app and export a copy

`codeaf app show` lists the app's declared views and Actions. `codeaf app query` reads a declared view with its required data grant. `codeaf app invoke` can call the registered edit, duplicate and export Actions with the exact action grant. A record edit takes an expected revision and leaves the actor and changed fields in that record's history. A person-authored value holds against an AI edit.

Open a record for its export controls. Docs exports PDF and DOCX. Sheets exports CSV with calculated values. `codeaf app worksheet-window` reads a scoped cell range with its record revision. Slides exports PPTX and PDF. Canvas exports board text to PDF. Exports use the current scoped record values and carry an output receipt. On Desk, choose a destination in the native Save panel. To share outside Desk, send the exported file through your usual channel.
