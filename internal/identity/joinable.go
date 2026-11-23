package identity

import "os"

// Joinable accepts startup state plus personal memory and preferences, which
// merge into the new fleet (C30.4). Chats and unknown files still require the
// explicit replacement door; Pristine continues to mean genuinely unused.
func Joinable(home string) (bool, error) {
	rulesCopy := rules{}
	for key, judge := range startupRules {
		rulesCopy[key] = judge
	}
	rulesCopy["graph.db"] = memoryOnlyGraph
	// These files are relay selection and an identity-bound permission cache.
	// Neither contains work that needs the old identity, and the cached
	// permission cannot apply to the account we join.
	rulesCopy["sync-url"] = regularRelayMetadata
	rulesCopy["sync-storage"] = regularRelayMetadata
	for _, key := range []string{"provider-keys.ledger", "preferences-sync.ledger", "memory-sync.ledger", "teams-sync.ledger", "teams-traffic-sync.ledger", "state-sync.lock"} {
		rulesCopy[key] = never
	}
	used, err := within(rulesCopy)(home)
	return !used && err == nil, err
}

func memoryOnlyGraph(path string) (bool, error) {
	return databaseWork(path, graphStartupTables(), true)
}

func regularRelayMetadata(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return true, err
	}
	return !info.Mode().IsRegular(), nil
}
