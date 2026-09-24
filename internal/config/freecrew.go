package config

// FreeChatModel is the build's chat bottom rung while a known account is low.
const FreeChatModel = "qwen/qwen3.8-27b:free"

// CrewFree is a derived reading of the five free seats, never a saved preset.
const CrewFree = "free"

// The free table is hand-picked because the ordinary crew picker rejects zero
// tariffs; each seat must remain usable without an account top-up.
var freeCrewModels = map[string]string{
	ModelTierReflex:     "nvidia/nemotron-3.5-lightning:free",
	ModelTierLow:        "thinkingmachines/inkling-small:free",
	ModelTierWorker:     FreeChatModel,
	ModelTierHigh:       "thinkingmachines/inkling:free",
	ModelTierMastermind: FreeChatModel,
}

// freeTierModelAt is the only low-credit check beneath an unwritten table seat.
func freeTierModelAt(profileDir, family, tier string) string {
	if useFreeDefaultsAt(profileDir) && CrewPickAt(profileDir) == CrewPickTable {
		if _, stored := storedCrewWord(profileDir); !stored {
			if model := freeCrewModels[tier]; model != "" {
				return model
			}
		}
	}
	return defaultTierModel(family, tier)
}
