//! The rules a native web view lives by, kept free of Tauri types so every
//! rule is a plain unit test.
//!
//! WHY THE URL POLICY IS THE GATE. Tauri resolves a command's access from the
//! WINDOW label as well as the webview label (`RuntimeAuthority::resolve_access`
//! in tauri 2.12), so the `main` capability covers every child webview added to
//! the main window. What keeps a web page out is the origin: a request whose
//! Origin is not the app's own is `Origin::Remote`, our capabilities declare no
//! `remote` URLs, and `Webview::on_message` then refuses every command and every
//! plugin call. Tauri still injects its IPC bootstrap into child webviews and
//! defines it non-configurable, so the page can call `invoke`; it is refused.
//! A child view that reached an app origin would be `Origin::Local` and be
//! trusted, so NO navigation may reach an app origin, top-level or framed.

use tauri::Url;

/// The most characters a renderer-supplied URL may carry.
pub const MAX_URL: usize = 4096;
/// The most characters a pane id may carry; the view label is `web-<pane>`.
pub const MAX_PANE_ID: usize = 64;
/// The most native web views one app may hold at once.
pub const MAX_VIEWS: usize = 24;
/// A rectangle edge beyond this many logical pixels is not a real pane.
pub const MAX_EDGE: f64 = 16384.0;
/// The prefix every native web view label carries.
pub const LABEL_PREFIX: &str = "web-";

/// Why a URL or a request was refused. The renderer shows one fixed sentence
/// per reason; the raw URL is never echoed back into an error string.
#[derive(Clone, Copy, Debug, PartialEq, Eq, serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub enum Refusal {
    TooLong,
    Malformed,
    Scheme,
    NoHost,
    Credentials,
    AppOrigin,
}

impl Refusal {
    pub fn sentence(self) -> &'static str {
        match self {
            Refusal::TooLong => "That address is too long",
            Refusal::Malformed => "That is not a web address",
            Refusal::Scheme => "Only http and https pages open in codeaf",
            Refusal::NoHost => "That address has no site",
            Refusal::Credentials => "Addresses with a name and password are not opened",
            Refusal::AppOrigin => "codeaf's own pages cannot open in a web tab",
        }
    }
}

/// The hosts Tauri serves its custom protocols from where a platform maps them
/// onto http (Windows, Android) and the scheme names it serves elsewhere.
const APP_HOSTS: [&str; 4] = [
    "tauri.localhost",
    "ipc.localhost",
    "asset.localhost",
    "isolation.localhost",
];

fn is_loopback(host: &str) -> bool {
    matches!(host, "localhost" | "127.0.0.1" | "[::1]" | "::1")
}

/// The app's own origins: its custom protocol hosts plus the development
/// server, which `is_local_url` treats as local while it is configured.
#[derive(Clone, Debug, Default)]
pub struct AppOrigins {
    dev: Option<(String, String, Option<u16>)>,
}

impl AppOrigins {
    pub fn new(dev_url: Option<&Url>) -> Self {
        let dev = dev_url.map(|url| {
            (
                url.scheme().to_string(),
                url.host_str().unwrap_or("").to_ascii_lowercase(),
                url.port_or_known_default(),
            )
        });
        AppOrigins { dev }
    }

    fn contains(&self, url: &Url) -> bool {
        let host = url.host_str().unwrap_or("").to_ascii_lowercase();
        if APP_HOSTS.contains(&host.as_str()) {
            return true;
        }
        let Some((scheme, dev_host, dev_port)) = &self.dev else {
            return false;
        };
        // Loopback spellings are one machine: 127.0.0.1:1420 is the dev server too.
        let same_host = host == *dev_host || (is_loopback(&host) && is_loopback(dev_host));
        same_host && url.scheme() == scheme && url.port_or_known_default() == *dev_port
    }
}

/// The address a person or a link asked for, checked before any view loads it.
pub fn checked_url(raw: &str, app: &AppOrigins) -> Result<Url, Refusal> {
    if raw.len() > MAX_URL {
        return Err(Refusal::TooLong);
    }
    if raw.is_empty() || raw.chars().any(|c| c.is_control() || c.is_whitespace()) {
        return Err(Refusal::Malformed);
    }
    let url = Url::parse(raw).map_err(|_| Refusal::Malformed)?;
    if !matches!(url.scheme(), "http" | "https") {
        return Err(Refusal::Scheme);
    }
    if url.host_str().map_or(true, str::is_empty) {
        return Err(Refusal::NoHost);
    }
    if !url.username().is_empty() || url.password().is_some() {
        return Err(Refusal::Credentials);
    }
    if app.contains(&url) {
        return Err(Refusal::AppOrigin);
    }
    Ok(url)
}

/// Whether a view may follow a navigation the PAGE started (a link, a form, a
/// redirect the engine reports, or a frame). Frames may be about:blank or
/// about:srcdoc, which inherit the page's own remote origin; nothing else that
/// is not http(s) is followed, and credentials in a page-made link are allowed
/// because the page, not codeaf, chose them.
pub fn allows_navigation(url: &Url, app: &AppOrigins) -> bool {
    match url.scheme() {
        "http" | "https" => url.host_str().is_some_and(|h| !h.is_empty()) && !app.contains(url),
        "about" => matches!(url.path(), "blank" | "srcdoc"),
        _ => false,
    }
}

/// Only the app's own top-level webviews may drive web views: `main` and the
/// multiwindow lane's `w-<n>`. A web view (`web-*`) never qualifies, so even a
/// view that somehow became local could not reach these commands' effects.
pub fn is_trusted_caller(webview_label: &str, window_label: &str) -> bool {
    let top = |label: &str| {
        label == "main"
            || label
                .strip_prefix("w-")
                .is_some_and(|n| !n.is_empty() && n.bytes().all(|b| b.is_ascii_digit()))
    };
    !webview_label.starts_with(LABEL_PREFIX) && top(webview_label) && webview_label == window_label
}

/// A pane id becomes part of a native label, so it is a short id and nothing else.
pub fn checked_pane(pane: &str) -> Result<&str, &'static str> {
    let ok = !pane.is_empty()
        && pane.len() <= MAX_PANE_ID
        && pane
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_');
    if ok {
        Ok(pane)
    } else {
        Err("That pane cannot hold a web page")
    }
}

pub fn label_for(pane: &str) -> String {
    format!("{LABEL_PREFIX}{pane}")
}

/// A pane's rectangle in the window's logical pixels, as the renderer measured it.
#[derive(Clone, Copy, Debug, PartialEq, serde::Deserialize, serde::Serialize)]
pub struct Rect {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

impl Rect {
    /// Rounds to whole pixels and clips to the window, so a stale or hostile
    /// rectangle can never place a page outside the window it belongs to.
    pub fn clamped(self, window_width: f64, window_height: f64) -> Result<Rect, &'static str> {
        let values = [self.x, self.y, self.width, self.height];
        if values.iter().any(|v| !v.is_finite() || v.abs() > MAX_EDGE)
            || self.width < 0.0
            || self.height < 0.0
        {
            return Err("That pane has no usable size");
        }
        let x = self.x.round().clamp(0.0, window_width.max(0.0));
        let y = self.y.round().clamp(0.0, window_height.max(0.0));
        let width = self.width.round().min(window_width - x).max(0.0);
        let height = self.height.round().min(window_height - y).max(0.0);
        Ok(Rect {
            x,
            y,
            width,
            height,
        })
    }
}

/// Standard base64 (RFC 4648) for the snapshot data URL; a dependency for
/// thirty lines would be the larger risk.
pub fn base64(bytes: &[u8]) -> String {
    const TABLE: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut out = String::with_capacity(bytes.len().div_ceil(3) * 4);
    for chunk in bytes.chunks(3) {
        let n = (chunk[0] as u32) << 16
            | (*chunk.get(1).unwrap_or(&0) as u32) << 8
            | *chunk.get(2).unwrap_or(&0) as u32;
        for i in 0..4 {
            if i <= chunk.len() {
                out.push(TABLE[(n >> (18 - 6 * i) & 63) as usize] as char);
            } else {
                out.push('=');
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn dev() -> AppOrigins {
        AppOrigins::new(Some(&Url::parse("http://localhost:1420").unwrap()))
    }

    #[test]
    fn opens_plain_web_addresses() {
        for good in [
            "https://pkg.go.dev/encoding/json#Decoder",
            "http://example.com",
            "https://example.com:8443/a?b=1",
            "http://localhost:3000/",
            "http://127.0.0.1:8080",
            "https://[2001:db8::1]/",
        ] {
            assert!(checked_url(good, &dev()).is_ok(), "{good}");
        }
    }

    #[test]
    fn refuses_every_non_web_scheme() {
        for bad in [
            "file:///etc/passwd",
            "tauri://localhost/index.html",
            "ipc://localhost/engine_connection",
            "asset://localhost/%2Fetc%2Fpasswd",
            "javascript:alert(1)",
            "data:text/html,<script>1</script>",
            "blob:https://example.com/uuid",
            "about:blank",
            "ftp://example.com",
            "view-source:https://example.com",
            "chrome://settings",
        ] {
            assert_eq!(checked_url(bad, &dev()), Err(Refusal::Scheme), "{bad}");
        }
    }

    #[test]
    fn refuses_app_origins_on_every_spelling() {
        for bad in [
            "http://tauri.localhost/index.html",
            "https://tauri.localhost/",
            "http://ipc.localhost/engine_connection",
            "http://asset.localhost/x",
            "http://IPC.LOCALHOST/x",
            "http://localhost:1420/",
            "http://127.0.0.1:1420/#/settings",
            "http://[::1]:1420/",
        ] {
            assert_eq!(checked_url(bad, &dev()), Err(Refusal::AppOrigin), "{bad}");
        }
        // Without a development server only the protocol hosts are the app.
        assert!(checked_url("http://localhost:1420/", &AppOrigins::default()).is_ok());
    }

    #[test]
    fn refuses_malformed_long_and_credentialed_addresses() {
        assert_eq!(checked_url("", &dev()), Err(Refusal::Malformed));
        assert_eq!(checked_url("example.com", &dev()), Err(Refusal::Malformed));
        assert_eq!(
            checked_url("https://a.com/ b", &dev()),
            Err(Refusal::Malformed)
        );
        assert_eq!(
            checked_url("https://a.com/\nb", &dev()),
            Err(Refusal::Malformed)
        );
        assert_eq!(
            checked_url("https://user:pw@a.com/", &dev()),
            Err(Refusal::Credentials)
        );
        assert_eq!(
            checked_url("https://user@a.com/", &dev()),
            Err(Refusal::Credentials)
        );
        let long = format!("https://a.com/{}", "x".repeat(MAX_URL));
        assert_eq!(checked_url(&long, &dev()), Err(Refusal::TooLong));
    }

    #[test]
    fn page_navigation_never_reaches_app_or_local_schemes() {
        let app = dev();
        let ok = |s: &str| allows_navigation(&Url::parse(s).unwrap(), &app);
        assert!(ok("https://example.com/next"));
        assert!(ok("https://user:pw@example.com/"));
        assert!(ok("about:blank"));
        assert!(ok("about:srcdoc"));
        for bad in [
            "tauri://localhost/",
            "ipc://localhost/x",
            "asset://localhost/x",
            "http://tauri.localhost/",
            "http://localhost:1420/",
            "file:///etc/hosts",
            "data:text/html,x",
            "blob:https://example.com/u",
            "javascript:alert(1)",
            "about:config",
            "mailto:a@b.c",
        ] {
            assert!(!ok(bad), "{bad}");
        }
    }

    #[test]
    fn only_top_level_app_webviews_are_callers() {
        assert!(is_trusted_caller("main", "main"));
        assert!(is_trusted_caller("w-2", "w-2"));
        for (webview, window) in [
            ("web-p1", "main"),
            ("web-main", "main"),
            ("w-", "w-"),
            ("w-x", "w-x"),
            ("main", "w-2"),
            ("other", "other"),
            ("", ""),
        ] {
            assert!(!is_trusted_caller(webview, window), "{webview} in {window}");
        }
    }

    #[test]
    fn pane_ids_are_short_plain_ids() {
        assert_eq!(checked_pane("tab-12_a"), Ok("tab-12_a"));
        for bad in [
            "",
            "a/b",
            "a b",
            "../x",
            "a:b",
            "é",
            &"x".repeat(MAX_PANE_ID + 1),
        ] {
            assert!(checked_pane(bad).is_err(), "{bad}");
        }
        assert_eq!(label_for("p1"), "web-p1");
    }

    #[test]
    fn rectangles_stay_inside_the_window() {
        let r = Rect {
            x: 100.4,
            y: 50.6,
            width: 2000.0,
            height: 300.0,
        };
        assert_eq!(
            r.clamped(1200.0, 800.0),
            Ok(Rect {
                x: 100.0,
                y: 51.0,
                width: 1100.0,
                height: 300.0
            })
        );
        let off = Rect {
            x: -40.0,
            y: 900.0,
            width: 10.0,
            height: 10.0,
        };
        assert_eq!(
            off.clamped(1200.0, 800.0),
            Ok(Rect {
                x: 0.0,
                y: 800.0,
                width: 10.0,
                height: 0.0
            })
        );
        for bad in [f64::NAN, f64::INFINITY, MAX_EDGE * 2.0] {
            assert!(Rect {
                x: bad,
                y: 0.0,
                width: 1.0,
                height: 1.0
            }
            .clamped(800.0, 600.0)
            .is_err());
        }
        assert!(Rect {
            x: 0.0,
            y: 0.0,
            width: -1.0,
            height: 1.0
        }
        .clamped(800.0, 600.0)
        .is_err());
    }

    #[test]
    fn base64_matches_rfc_4648_vectors() {
        for (input, want) in [
            ("", ""),
            ("f", "Zg=="),
            ("fo", "Zm8="),
            ("foo", "Zm9v"),
            ("foob", "Zm9vYg=="),
            ("fooba", "Zm9vYmE="),
            ("foobar", "Zm9vYmFy"),
        ] {
            assert_eq!(base64(input.as_bytes()), want);
        }
    }
}
