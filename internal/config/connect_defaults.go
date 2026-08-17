package config

// The Google registration this build ships with, and the law that lets it.
//
// A DESKTOP CLIENT'S SECRET IS NOT A SECRET. Google's own guidance for the
// installed-application flow says so plainly: a program that runs on the
// person's own machine cannot hold anything back from the person running it, so
// the "secret" of such a client is treated as embeddable and the flow is built
// to be safe without it — every sign-in still happens in the person's browser,
// under their own Google account, and nothing is granted until they approve it
// there. What the pair names is WHICH APPLICATION IS ASKING, not who may say
// yes.
//
// They are compiled in so that a fresh clone connects an account with no
// per-machine setup at all — the alternative is every person registering their
// own application with Google before they can read their own mail, which is a
// registration form standing between somebody and the first useful thing.
//
// THEY ARE THE LAST RUNG AND NOTHING ELSE, so a person or a deployment that
// wants its own registration simply answers for it: GOOGLE_OAUTH_CLIENT and
// GOOGLE_OAUTH_SECRET in the environment, or the google_oauth_client and
// google_oauth_secret rows in the profile config, both of which still win
// ([GoogleOAuthClientAt]). The settings sheet keeps reading the person's own
// answer alone, so an unanswered row still says "not set" rather than claiming
// a value the person never wrote.
//
// THIS PAIR MUST BE ROTATED BEFORE ANY PUBLIC DISTRIBUTION. What is shared here
// is one quota, one consent screen, and one revocation: a shipped id that is
// abused is disabled for everybody holding the same binary, so a build that
// goes out beyond this team registers its own client and replaces these two
// lines.
const (
	defaultGoogleOAuthClient = "173863316211-5jct1pmtq8bn4crsl029slkv9g4r8b4q.apps.googleusercontent.com"
	defaultGoogleOAuthSecret = "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"
)
