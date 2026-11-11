package factory

// RecipeFiles supplies the YAML writer without making the factory import its
// pipeline interpreter. Markdown has no writer; it remains compatibility input.
type RecipeFiles interface {
	Save(dir string, recipe Recipe) error
	BankStages(dir string, kind Kind, stages []Stage) error
	BankRule(dir, sentence string) error
	WriteDefault(dir string) error
}

// WithRecipeFiles gives the local seam its recipe writes. Without a writer,
// saving and banking stay absent, even when a checkout is known.
func WithRecipeFiles(files RecipeFiles) LocalOption {
	return func(o *localOptions) { o.files = files }
}

// WithOptions combines related doors supplied by one interpreter.
func WithOptions(options ...LocalOption) LocalOption {
	return func(o *localOptions) {
		for _, option := range options {
			if option != nil {
				option(o)
			}
		}
	}
}
