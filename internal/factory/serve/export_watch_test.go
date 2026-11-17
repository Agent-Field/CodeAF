package serve

// WatchesItsRoot says whether h follows its root with a filesystem watch. A
// host short of inotify instances opens none, and its pulls keep the timed
// look ([Handler.blindLooks]).
func WatchesItsRoot(h *Handler) bool { return h.watch != nil && h.watch.fs != nil }

// WorkerPollEvery is the timed look of a pull with no filesystem watch.
const WorkerPollEvery = workerPollEvery
