# Desktop image File view

## How do I open an image file or SVG in the desktop File view?

Open the image file from the new tab's search field or a file link, then choose
File if the tab is showing Changes. Image file kinds include PNG, JPEG, GIF,
WebP, AVIF, BMP, ICO and SVG. The picture is centred on the canvas, at natural
size, and shrinks to fit the pane without stretching or upscaling. Transparent
areas show the canvas, without a checkerboard.

The bytes come from the conversation's engine through its existing confined
file read, even when that engine is remote. SVG is an image loaded from a blob
URL; its markup is never inserted into the app's document and its scripts do
not execute.

## Why cannot my desktop image be shown? How do I open an oversized picture?

The engine's file read is capped at 16 MiB. A larger answer shows
"Too large to show ·" followed by the file size; an engine refusal shows its
reason. A picture that fails to decode shows "This image cannot be shown."
An unsupported MIME type shows "Binary file". Each refusal keeps the
"Open in" dropdown available, with the engine's editors and "Copy path".
The view never invents editors that the engine has not reported.
