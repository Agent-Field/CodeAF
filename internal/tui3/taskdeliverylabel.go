package tui3

// Work can be accepted while its files still failed to reach their destination.
// A retained branch alone is not a failure: keeping it may be the requested
// delivery. Only an explicit conflict or aborted integration earns this label.
const doneDeliveryWord = "delivery needs attention"

func (card *taskDone) deliveryProblem() bool {
	return card.merge == mergeWordConflicted || card.merge == mergeWordAborted
}
