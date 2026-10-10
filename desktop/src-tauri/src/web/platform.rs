//! What Tauri does not expose about a web view, read from the platform view
//! itself: history state, back/forward/stop, load failures and a snapshot.
//! Every call runs on the main thread through `Webview::with_webview` and
//! reports through a callback, so no command ever blocks the event loop.
//! A platform without an implementation answers honestly: history state is
//! unknown and a snapshot is unavailable.

use tauri::{Runtime, Webview};

/// The widest snapshot handed to the renderer, in pixels. A hover card is
/// 300px wide and an overview card smaller, so this covers a 2x display.
pub const SNAPSHOT_WIDTH: u32 = 640;
/// A snapshot larger than this once encoded is dropped rather than shipped.
pub const SNAPSHOT_BYTES: usize = 1_500_000;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Probe {
    pub can_back: bool,
    pub can_forward: bool,
    pub loading: bool,
    /// False where the platform cannot say; the renderer then keeps Back and
    /// Forward enabled rather than claiming there is no history.
    pub known: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Step {
    Back,
    Forward,
    Stop,
}

/// Scales a page to the snapshot width, keeping its aspect ratio.
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub fn snapshot_size(width: u32, height: u32) -> (u32, u32) {
    if width == 0 || height == 0 {
        return (0, 0);
    }
    let target = width.min(SNAPSHOT_WIDTH);
    let scaled = (height as u64 * target as u64 / width as u64).max(1) as u32;
    (target, scaled)
}

pub type Done<T> = Box<dyn FnOnce(T) + Send + 'static>;

#[cfg(target_os = "linux")]
mod imp {
    use super::*;
    use gtk::{cairo, gdk, gdk_pixbuf::InterpType};
    use webkit2gtk::{SnapshotOptions, SnapshotRegion, WebViewExt};

    pub fn probe<R: Runtime>(view: &Webview<R>, done: Done<Probe>) {
        let _ = view.with_webview(move |platform| {
            let wv = platform.inner();
            done(Probe {
                can_back: wv.can_go_back(),
                can_forward: wv.can_go_forward(),
                loading: wv.is_loading(),
                known: true,
            });
        });
    }

    pub fn step<R: Runtime>(view: &Webview<R>, step: Step) {
        let _ = view.with_webview(move |platform| {
            let wv = platform.inner();
            match step {
                Step::Back => wv.go_back(),
                Step::Forward => wv.go_forward(),
                Step::Stop => wv.stop_loading(),
            }
        });
    }

    /// WebKitGTK reports failed loads; cancellations and policy refusals are
    /// ours (a stop, a refused navigation, a refused download) and are not failures.
    pub fn watch_failures<R: Runtime>(view: &Webview<R>, failed: Box<dyn Fn() + Send + 'static>) {
        let _ = view.with_webview(move |platform| {
            platform.inner().connect_load_failed(move |_, _, _, error| {
                let ours = error.matches(webkit2gtk::NetworkError::Cancelled)
                    || error.kind::<webkit2gtk::PolicyError>().is_some();
                if !ours {
                    failed();
                }
                false
            });
        });
    }

    pub fn snapshot<R: Runtime>(view: &Webview<R>, done: Done<Result<Vec<u8>, &'static str>>) {
        let _ = view.with_webview(move |platform| {
            platform.inner().snapshot(
                SnapshotRegion::Visible,
                SnapshotOptions::NONE,
                None::<&webkit2gtk::gio::Cancellable>,
                move |result| done(result.map_err(|_| "unavailable").and_then(encode)),
            );
        });
    }

    fn encode(surface: cairo::Surface) -> Result<Vec<u8>, &'static str> {
        let image = cairo::ImageSurface::try_from(surface).map_err(|_| "unavailable")?;
        let (width, height) = (image.width(), image.height());
        let pixbuf =
            gdk::pixbuf_get_from_surface(&image, 0, 0, width, height).ok_or("unavailable")?;
        let (w, h) = snapshot_size(width as u32, height as u32);
        let scaled = pixbuf
            .scale_simple(w as i32, h as i32, InterpType::Bilinear)
            .ok_or("unavailable")?;
        scaled
            .save_to_bufferv("png", &[])
            .map_err(|_| "unavailable")
    }
}

#[cfg(target_os = "macos")]
mod imp {
    use super::*;
    use block2::RcBlock;
    use objc2_app_kit::{NSBitmapImageFileType, NSBitmapImageRep, NSImage};
    use objc2_foundation::{MainThreadMarker, NSDictionary, NSError, NSNumber};
    use objc2_web_kit::{WKSnapshotConfiguration, WKWebView};
    use std::sync::Mutex;

    // SAFETY: wry hands out its WKWebView pointer, alive for the closure.
    fn web_view(platform: &tauri::webview::PlatformWebview) -> &WKWebView {
        unsafe { &*(platform.inner() as *const WKWebView) }
    }

    pub fn probe<R: Runtime>(view: &Webview<R>, done: Done<Probe>) {
        let _ = view.with_webview(move |platform| {
            let wv = web_view(&platform);
            let probe = unsafe {
                Probe {
                    can_back: wv.canGoBack(),
                    can_forward: wv.canGoForward(),
                    loading: wv.isLoading(),
                    known: true,
                }
            };
            done(probe);
        });
    }

    pub fn step<R: Runtime>(view: &Webview<R>, step: Step) {
        let _ = view.with_webview(move |platform| {
            let wv = web_view(&platform);
            unsafe {
                match step {
                    Step::Back => drop(wv.goBack()),
                    Step::Forward => drop(wv.goForward()),
                    Step::Stop => wv.stopLoading(),
                }
            }
        });
    }

    /// WKWebView failures go to wry's navigation delegate, which keeps them;
    /// the caller detects them by polling `probe` instead (see web.rs).
    pub fn watch_failures<R: Runtime>(_view: &Webview<R>, _failed: Box<dyn Fn() + Send + 'static>) {
    }

    pub fn snapshot<R: Runtime>(view: &Webview<R>, done: Done<Result<Vec<u8>, &'static str>>) {
        let _ = view.with_webview(move |platform| {
            let wv = web_view(&platform);
            let Some(mtm) = MainThreadMarker::new() else {
                return done(Err("unavailable"));
            };
            let config = unsafe { WKSnapshotConfiguration::new(mtm) };
            unsafe {
                config.setSnapshotWidth(Some(&NSNumber::new_f64(SNAPSHOT_WIDTH as f64 / 2.0)))
            };
            // The block is Fn; the result is delivered once.
            let once = Mutex::new(Some(done));
            let block = RcBlock::new(move |image: *mut NSImage, _error: *mut NSError| {
                let Some(done) = once.lock().ok().and_then(|mut slot| slot.take()) else {
                    return;
                };
                done(
                    unsafe { image.as_ref() }
                        .ok_or("unavailable")
                        .and_then(encode),
                );
            });
            unsafe { wv.takeSnapshotWithConfiguration_completionHandler(Some(&config), &block) };
        });
    }

    fn encode(image: &NSImage) -> Result<Vec<u8>, &'static str> {
        let tiff = image.TIFFRepresentation().ok_or("unavailable")?;
        let rep = NSBitmapImageRep::imageRepWithData(&tiff).ok_or("unavailable")?;
        let png = unsafe {
            rep.representationUsingType_properties(NSBitmapImageFileType::PNG, &NSDictionary::new())
        };
        Ok(png.ok_or("unavailable")?.to_vec())
    }
}

#[cfg(not(any(target_os = "linux", target_os = "macos")))]
mod imp {
    use super::*;

    pub fn probe<R: Runtime>(_view: &Webview<R>, done: Done<Probe>) {
        done(Probe::default());
    }

    // Without a platform history handle, the page's own history is the honest fallback.
    pub fn step<R: Runtime>(view: &Webview<R>, step: Step) {
        let _ = view.eval(match step {
            Step::Back => "history.back()",
            Step::Forward => "history.forward()",
            Step::Stop => "window.stop()",
        });
    }

    pub fn watch_failures<R: Runtime>(_view: &Webview<R>, _failed: Box<dyn Fn() + Send + 'static>) {
    }

    pub fn snapshot<R: Runtime>(_view: &Webview<R>, done: Done<Result<Vec<u8>, &'static str>>) {
        done(Err("unavailable"));
    }
}

pub use imp::{probe, snapshot, step, watch_failures};

/// Where a child view sits. macOS and Windows place a child webview at its
/// bounds themselves. Tauri 2.12 on Linux packs a child into the window's
/// GtkBox beside the app's own webview (`build_gtk(vbox)` in
/// tauri-runtime-wry), where wry's `set_bounds` does nothing: the page would
/// split the window instead of covering its pane. So on Linux the app's
/// webview moves into a GtkOverlay in its place, and each web view becomes an
/// overlay child, placed by margins and a size request over its pane.
#[cfg(target_os = "linux")]
mod layer {
    use super::super::policy::Rect;
    use gtk::prelude::*;
    use tauri::{Runtime, Webview};

    /// Where each page remembers the rectangle it was last asked to cover, so
    /// the overlay can place it again whenever the overlay's own size changes.
    const RECT: &str = "codeaf-web-rect";

    /// Fills `width` x `height` less four margins, so the page covers exactly
    /// `rect`. A size request alone is not enough: an overlay child gets its
    /// natural size, and WebKit reports its last content size as natural.
    fn margins(page: &gtk::Widget, rect: Rect, width: i32, height: i32) {
        let (x, y) = (rect.x as i32, rect.y as i32);
        let (w, h) = ((rect.width as i32).max(1), (rect.height as i32).max(1));
        page.set_halign(gtk::Align::Fill);
        page.set_valign(gtk::Align::Fill);
        page.set_margin_start(x);
        page.set_margin_top(y);
        page.set_margin_end((width - x - w).max(0));
        page.set_margin_bottom((height - y - h).max(0));
    }

    fn position(page: &gtk::Widget, rect: Rect) {
        // SAFETY: the key is private to this module and always holds a `Rect`.
        unsafe { page.set_data(RECT, rect) };
        let Some(overlay) = page.parent() else { return };
        let base = overlay
            .downcast_ref::<gtk::Overlay>()
            .and_then(|o| o.child())
            .map(|app| (app.allocated_width(), app.allocated_height()));
        let width = overlay.allocated_width().max(base.map_or(0, |b| b.0));
        let height = overlay.allocated_height().max(base.map_or(0, |b| b.1));
        // An overlay made a moment ago has no size yet, and margins measured
        // against nothing leave the end and bottom at zero: the first page ran
        // to the window's edge until something resized it. Its size-allocate
        // handler places the page once the size is known.
        if width <= 1 || height <= 1 {
            return;
        }
        margins(page, rect, width, height);
    }

    /// Places every page on `overlay` again against the overlay's new size.
    /// Margins that already match change nothing, so this settles at once.
    fn reflow(overlay: &gtk::Overlay, width: i32, height: i32) {
        let app = overlay.child();
        for page in overlay.children() {
            if app.as_ref() == Some(&page) {
                continue;
            }
            // SAFETY: only `position` writes this key, and always a `Rect`.
            let Some(rect) = (unsafe { page.data::<Rect>(RECT) }) else {
                continue;
            };
            margins(&page, unsafe { *rect.as_ref() }, width, height);
        }
    }

    /// Moves `view` out of the window's box onto an overlay above `main`.
    pub fn adopt<R: Runtime>(main: &Webview<R>, view: &Webview<R>, rect: Rect) {
        let _ = main.with_webview(|platform| {
            let app: gtk::Widget = platform.inner().upcast();
            let Some(parent) = app.parent() else { return };
            if parent.is::<gtk::Overlay>() {
                return;
            }
            let Ok(column) = parent.downcast::<gtk::Box>() else {
                return;
            };
            let position = column
                .children()
                .iter()
                .position(|c| c == &app)
                .unwrap_or(0);
            let overlay = gtk::Overlay::new();
            // A margin set inside an allocation is not laid out until something
            // else asks, so the overlay places its pages once allocation is over.
            overlay.connect_size_allocate(|overlay, _| {
                let overlay = overlay.clone();
                gtk::glib::idle_add_local_once(move || {
                    reflow(
                        &overlay,
                        overlay.allocated_width(),
                        overlay.allocated_height(),
                    )
                });
            });
            column.remove(&app);
            overlay.add(&app);
            column.pack_start(&overlay, true, true, 0);
            column.reorder_child(&overlay, position as i32);
            overlay.show();
        });
        let _ = view.with_webview(move |platform| {
            let page: gtk::Widget = platform.inner().upcast();
            let Some(column) = page.parent().and_then(|p| p.downcast::<gtk::Box>().ok()) else {
                return;
            };
            let Some(overlay) = column
                .children()
                .into_iter()
                .find_map(|c| c.downcast::<gtk::Overlay>().ok())
            else {
                return;
            };
            let shown = page.is_visible();
            column.remove(&page);
            overlay.add_overlay(&page);
            position(&page, rect);
            page.set_visible(shown);
        });
    }

    pub fn place<R: Runtime>(view: &Webview<R>, rect: Rect) {
        let _ = view.with_webview(move |platform| position(&platform.inner().upcast(), rect));
    }
}

#[cfg(not(target_os = "linux"))]
mod layer {
    use super::super::policy::Rect;
    use tauri::{Runtime, Webview};

    pub fn adopt<R: Runtime>(_main: &Webview<R>, _view: &Webview<R>, _rect: Rect) {}
    pub fn place<R: Runtime>(_view: &Webview<R>, _rect: Rect) {}
}

pub use layer::{adopt, place};

/// True where a load failure only shows as loading stopping without a finish.
pub const POLLS_FOR_FAILURE: bool = cfg!(target_os = "macos");

/// On Linux a key pressed in the page never reaches the app's document, and
/// there is no menu bar to catch it. The three app chords are window
/// accelerators; the page forwards them because WebKit would otherwise keep
/// the key. macOS uses the menu, which already runs with the page focused.
#[cfg(target_os = "linux")]
mod chords {
    use super::super::policy::{self, AppChord};
    use super::{Runtime, Webview};
    use gtk::gdk;
    use gtk::prelude::{AccelGroupExtManual, *};
    use std::cell::RefCell;
    use tauri::AppHandle;

    /// Marks a child page so a bubbled key is not treated as the app's own view.
    const WEB_PAGE: &str = "codeaf-web-page";
    /// Keeps the accelerator group alive for the window.
    const HELD: &str = "codeaf-web-accels";

    thread_local! {
        static FORWARDING: RefCell<bool> = const { RefCell::new(false) };
        static CHORD_PANE: RefCell<Option<String>> = const { RefCell::new(None) };
    }

    struct Held {
        _group: gtk::AccelGroup,
        _closures: Vec<gtk::glib::Closure>,
    }

    /// Clears the forward flag even when activation returns early.
    struct Forwarding;

    impl Forwarding {
        fn begin(pane: &str) -> Self {
            FORWARDING.with(|slot| *slot.borrow_mut() = true);
            CHORD_PANE.with(|slot| *slot.borrow_mut() = Some(pane.to_string()));
            Forwarding
        }
    }

    impl Drop for Forwarding {
        fn drop(&mut self) {
            FORWARDING.with(|slot| *slot.borrow_mut() = false);
            CHORD_PANE.with(|slot| *slot.borrow_mut() = None);
        }
    }

    fn letter(key: gdk::keys::Key) -> Option<char> {
        let key = *key;
        if key == *gdk::keys::constants::t || key == *gdk::keys::constants::T {
            Some('t')
        } else if key == *gdk::keys::constants::w || key == *gdk::keys::constants::W {
            Some('w')
        } else if key == *gdk::keys::constants::l || key == *gdk::keys::constants::L {
            Some('l')
        } else {
            None
        }
    }

    fn focus_is_web_page(window: &gtk::Window) -> bool {
        let mut widget = window.focused_widget();
        while let Some(current) = widget {
            if unsafe { current.data::<bool>(WEB_PAGE) }.is_some() {
                return true;
            }
            widget = current.parent();
        }
        false
    }

    fn ensure<R: Runtime>(app: &AppHandle<R>, owner: &str, window: &gtk::Window) {
        if unsafe { window.data::<Held>(HELD) }.is_some() {
            return;
        }
        let group = gtk::AccelGroup::new();
        window.add_accel_group(&group);
        let mut closures = Vec::new();
        let app = app.clone();
        let owner = owner.to_string();
        for (key, chord) in [
            (*gdk::keys::constants::t, AppChord::NewTab),
            (*gdk::keys::constants::w, AppChord::CloseTab),
            (*gdk::keys::constants::l, AppChord::Address),
        ] {
            let app = app.clone();
            let owner = owner.clone();
            let closure = <gtk::AccelGroup as AccelGroupExtManual>::connect_accel_group(
                &group,
                key,
                gdk::ModifierType::CONTROL_MASK,
                gtk::AccelFlags::empty(),
                move |_, target, _, _| {
                    let forwarded = FORWARDING.with(|slot| *slot.borrow());
                    let Some(window) = target.downcast_ref::<gtk::Window>() else {
                        return false;
                    };
                    // A key that bubbled out of the app's own view is already
                    // handled there. Only a page, or a page forwarding, counts.
                    if !forwarded && !focus_is_web_page(window) {
                        return false;
                    }
                    let pane = CHORD_PANE.with(|slot| slot.borrow().clone());
                    super::super::deliver_chord(&app, &owner, pane.as_deref(), chord);
                    true
                },
            );
            closures.push(closure);
        }
        unsafe {
            window.set_data(
                HELD,
                Held {
                    _group: group,
                    _closures: closures,
                },
            );
        }
    }

    pub fn bind_app_chords<R: Runtime>(
        app: &AppHandle<R>,
        owner: &str,
        view: &Webview<R>,
        pane: &str,
    ) {
        let app = app.clone();
        let owner = owner.to_string();
        let pane = pane.to_string();
        let _ = view.with_webview(move |platform| {
            let page: gtk::Widget = platform.inner().upcast();
            unsafe { page.set_data(WEB_PAGE, true) };
            let Some(window) = page
                .toplevel()
                .and_then(|top| top.downcast::<gtk::Window>().ok())
            else {
                return;
            };
            ensure(&app, &owner, &window);
            let pane = pane.clone();
            let window = window.clone();
            page.connect_key_press_event(move |_page, event| {
                let mods = event.state() & gtk::accelerator_get_default_mod_mask();
                let Some(letter) = letter(event.keyval()) else {
                    return gtk::glib::Propagation::Proceed;
                };
                let command = mods.contains(gdk::ModifierType::SUPER_MASK)
                    || mods.contains(gdk::ModifierType::META_MASK);
                if policy::app_chord(
                    false,
                    letter,
                    command,
                    mods.contains(gdk::ModifierType::CONTROL_MASK),
                    mods.contains(gdk::ModifierType::SHIFT_MASK),
                    mods.contains(gdk::ModifierType::MOD1_MASK),
                )
                .is_none()
                {
                    return gtk::glib::Propagation::Proceed;
                }
                // Activate the registered chord, not the raw keyval: Caps Lock
                // reports T, and the accelerator is bound to t.
                let key = match letter {
                    't' => *gdk::keys::constants::t,
                    'w' => *gdk::keys::constants::w,
                    _ => *gdk::keys::constants::l,
                };
                let _forward = Forwarding::begin(&pane);
                let _ = gtk::accel_groups_activate(&window, key, gdk::ModifierType::CONTROL_MASK);
                gtk::glib::Propagation::Stop
            });
        });
    }
}

#[cfg(target_os = "linux")]
pub use chords::bind_app_chords;

#[cfg(not(target_os = "linux"))]
pub fn bind_app_chords<R: Runtime>(
    _app: &tauri::AppHandle<R>,
    _owner: &str,
    _view: &Webview<R>,
    _pane: &str,
) {
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn snapshots_scale_down_and_keep_aspect() {
        assert_eq!(snapshot_size(1280, 800), (640, 400));
        assert_eq!(snapshot_size(320, 900), (320, 900));
        assert_eq!(snapshot_size(4000, 1), (640, 1));
        assert_eq!(snapshot_size(0, 10), (0, 0));
    }
}
