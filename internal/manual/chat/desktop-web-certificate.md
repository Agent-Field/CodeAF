# Desktop web certificate errors

## This site's certificate isn't trusted in a desktop web tab

When the desktop receives a certificate failure for a web tab, it hides the page
and shows “This site's certificate isn't trusted.” in muted text. The tab gets a
failed glyph; this does not request an answer from you. Open in browser opens the
same address in your browser. Reload retries the page. There is no trust bypass
or modal error dialog.

The current native loader reports unreachable or refused failures without a
separate certificate classification. Those keep their existing generic error;
the certificate sentence appears only when a certificate failure is supplied.
