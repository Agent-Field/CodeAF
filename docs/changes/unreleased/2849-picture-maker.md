---
kind: added
title: The picture maker can be chosen Everywhere or for one canvas, and a chosen one is never swapped
pr: 2849
surface: [engine, app, chat, docs]
invalidates:
  - "The model that draws was one setting, the image slot, and the desk had no way to list drawing models with their price or capabilities. pictures.makers now lists every drawing model with its price per picture, whether it reads a photo, how many pictures one request returns, its shapes and a transparent background, and pictures.maker.set chooses one Everywhere (the image slot) or for one canvas, named by its working folder. Empty at either reach is Automatic, and the answer names the model Automatic would use."
  - "generate_image used exactly one model and failed when it did not answer. On Automatic it now passes over a model that is busy, unreachable or gone for the next on the same ladder (at most three) and says so (`Made with X · Automatic · Y was busy.`); renders carry picked and passed_over. A model somebody chose is never swapped: nothing is made and the render refuses with chosen_not_answering, naming the one model the person may switch to."
  - "A chosen model that cannot read a photo was sent the photo anyway. Changing a picture with one is now refused before anything is paid, with cannot_read_photos and a sentence naming a model that can."
  - "In a conversation, a picture with no default said `there is no default image model with a published price …` or `no connected service advertises image generation`. It now says `No picture maker has a published price, so Automatic won't choose one for you. Choose a picture maker in Settings → Models.` or `No connected provider makes pictures. Connect one in Settings → Models.` instead."
---
