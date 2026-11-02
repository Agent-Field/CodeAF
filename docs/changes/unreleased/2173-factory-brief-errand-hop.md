---
kind: fixed
pr: 2173
title: A factory brief no longer fails as "unavailable" when its model takes more than ten seconds
surface: [engine, app]
invalidates:
  - "An errand over the remote hop (conversation.errand) waited only the ordinary ten-second call deadline, so a brief writer whose model took longer came back \"unavailable\". The near end now waits the errand's own deadline plus ten seconds."
  - "A far end's refusal of an errand ask (ErrErrandAsk) crossed the hop as bare text and became \"unavailable\". It now keeps its kind and words, like a route refusal."
  - "A remote owner without errands answered Reason unavailable with no error. It now returns an error that says the engine does not run errands; the app still answers unavailable."
  - "The app's errand door now logs every owner failure on the engine log as `codeaf app: errand <purpose> on agent <id> answered <reason>: <error>`, so a failed brief's real reason can be read."
---
