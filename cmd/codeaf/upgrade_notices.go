package main

import (
	"log"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/upgradenotice"
)

// v3UpgradeNotice runs on the machine holding the home, only when a surface
// opens. A daemon boot or a headless message cannot consume recovery news.
func v3UpgradeNotice(root string, interactive bool) string {
	standing, err := upgradenotice.Standing(home.Dir(), interactive)
	if err != nil {
		log.Printf("standing upgrade report could not be saved: %v", err)
	}
	older, err := cell.OlderTurnsNotice(root, interactive)
	if err != nil {
		log.Printf("older chat report could not be saved: %v", err)
	}
	return joinV3Notices(standing, older)
}
