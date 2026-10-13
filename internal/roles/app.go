package roles

import (
	"fmt"
	"sort"
	"strings"
)

// RegisterApp is the explicit permission for an app feature to buy tokens.
// NO APP ROLE SHIPS BY DEFAULT. Owner-approved features register here through
// this door; a caller-supplied app prefix alone is never permission.
func RegisterApp(role Role, tier Tier) error {
	if !strings.HasPrefix(string(role), "app.") || strings.TrimPrefix(string(role), "app.") == "" || strings.ContainsAny(string(role), " \t\r\n") {
		return fmt.Errorf("app roles need an app.<purpose> name")
	}
	if tier != TierReflex && tier != TierLow && tier != TierWorker {
		return fmt.Errorf("app roles use reflex, low or worker")
	}
	Register(role, tier)
	return nil
}

// AppRoles lists only explicitly registered app features, for the token-path law.
func AppRoles() []Role {
	registryMu.RLock()
	defer registryMu.RUnlock()
	var result []Role
	for role := range registry {
		if strings.HasPrefix(string(role), "app.") {
			result = append(result, role)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// ResolveAppCall keeps the ordinary pin, tier, default ladder. An optional tier
// changes only its middle rung; it never gives the app a model-selection verb.
func ResolveAppCall(src Source, role Role, override Tier, fallback string) (Call, error) {
	tier, ok := TierOf(role)
	if !ok || !strings.HasPrefix(string(role), "app.") {
		return Call{}, fmt.Errorf("%w: %q", ErrUnknownRole, role)
	}
	if override != "" && override != TierReflex && override != TierLow && override != TierWorker {
		return Call{}, fmt.Errorf("app tier must be reflex, low or worker")
	}
	if override != "" {
		original := src
		src = func(key string) (string, bool) {
			if key == TierKey(tier) {
				key = TierKey(override)
			}
			return read(original, key)
		}
	}
	return ResolveCall(src, role, fallback)
}
