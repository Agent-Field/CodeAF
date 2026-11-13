package session

import "strconv"

// appRunPlanAliases links store ids to the run rows already in the listing.
// Plan-only ids keep their bytes, including nonnumeric parent ids.
func appRunPlanAliases(rows []TaskNotice) map[string]string {
	aliases := make(map[string]string, len(rows))
	for _, row := range rows {
		aliases[planStoreID(appRunRowKey(row))] = strconv.FormatUint(row.ID, 10)
	}
	return aliases
}

// appPlanGroups inherits only a group's recorded value, never a guessed run id.
func appPlanGroups(runs []TaskNotice, plans []PlanTaskRow) map[string]string {
	groups := make(map[string]string, len(plans))
	parents := make(map[string]string, len(plans))
	for _, run := range runs {
		groups[planStoreID(appRunRowKey(run))] = run.Run
	}
	for _, plan := range plans {
		parents[plan.ID] = plan.Parent
	}
	for _, plan := range plans {
		for id, n := plan.ID, 0; id != "" && n <= len(parents); id, n = parents[id], n+1 {
			if group := groups[id]; group != "" {
				groups[plan.ID] = group
				break
			}
		}
	}
	return groups
}
