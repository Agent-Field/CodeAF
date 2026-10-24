package relayserve

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
)

// The self-hosted page follows the Worker's /p/ page: the key stays in the
// fragment and the page performs no request. Its command also names this
// service so approval cannot send the mailbox code to the hosted default.
const pairPageStyle = `body{font:16px/1.5 system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem}code{display:block;margin:1rem 0;padding:.75rem;background:#eee;color:#111;overflow-wrap:anywhere}button{font:inherit;padding:.5rem 1rem}@media(prefers-color-scheme:dark){body{background:#111;color:#eee}}`

const pairPageScript = `const code=location.pathname.split('/').pop();
const key=location.hash.slice(1);
const token=key?code+'.'+key:code;
const via=location.origin+location.pathname.slice(0,location.pathname.lastIndexOf('/p/'));
document.getElementById('cmd').textContent='codeaf pair approve '+token+' --via '+via;
document.getElementById('open').onclick=()=>{location.href='codeaf://pair?code='+encodeURIComponent(code)+'&via='+encodeURIComponent(via)+(key?'#'+key:'');};`

const pairPageBody = `<h1>Add this device</h1>
<p>Open this on your computer: run</p>
<code id="cmd">codeaf pair approve &lt;paste the link&gt;</code>
<p>Or, if codeaf is installed here:</p>
<button id="open" type="button">Open in codeaf</button>`

var pairPageHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Add this device</title><style>` + pairPageStyle + `</style></head><body>` + pairPageBody + `<script>` + pairPageScript + `</script></body></html>`

func pageHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

var pairPageCSP = "default-src 'none'; style-src " + pageHash(pairPageStyle) + "; script-src " + pageHash(pairPageScript) + "; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func servePairPage(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/p/")
	if r.Method != http.MethodGet || code == "" || strings.Contains(code, "/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", pairPageCSP)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(pairPageHTML))
}
