package config

// FreeChatModel is the build's chat bottom rung while a known account is low.
const FreeChatModel = "qwen/qwen3.8-27b:free"

// freeHelperModels are the free models the two rows that are not crew seats —
// reflex and small work — read while a known account is low. The table is
// hand-picked because an ordinary pick rejects zero tariffs; each row must
// stay usable without an account top-up.
//
// The three crew seats are not here: they are routed per task, and a known-low
// account reaches the router as an OpenRouter account out of credit
// ([crewHealthAt]), which moves the seats onto free routes with the router's
// own notice.
var freeHelperModels = map[string]string{
	ModelTierReflex: "nvidia/nemotron-3.5-lightning:free",
	ModelTierLow:    "thinkingmachines/inkling-small:free",
}

// freeTierModelAt is the only low-credit check beneath an unwritten helper row.
func freeTierModelAt(profileDir, tier string) string {
	if useFreeDefaultsAt(profileDir) {
		if model := freeHelperModels[tier]; model != "" {
			return model
		}
	}
	return builtinTierModel(tier)
}
