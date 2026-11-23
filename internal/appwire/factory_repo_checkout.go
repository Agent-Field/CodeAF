package appwire

// FeatureFactoryRepoCheckout is announced in hello when this engine's
// factory.queue carries, on each of its repos, checkout_state: how a run of
// that repository gets its folder on the factory's machine, in the words a
// job's own checkout says (here, will_clone or missing). It lets a Start gate
// check a repository that has no jobs yet. A repository no job has run on is
// will_clone, because the runner clones it on the first job. The word is
// absent on a factory that does not know its machine's checkouts and on every
// attached factory older than this engine, and a surface then shows nothing
// about the checkout rather than a guess.
const FeatureFactoryRepoCheckout = "factory.repo_checkout"

func withFactoryRepoCheckoutFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); ok {
		hello = withFeature(hello, FeatureFactoryRepoCheckout)
	}
	return hello
}
