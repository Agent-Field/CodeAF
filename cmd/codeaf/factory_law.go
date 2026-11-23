package main

// talkRecipeLaw is the one sentence the item manager's brief carries about the
// team's recipe. It is its own constant so the brief (factory_talk.go) can
// include it with [talkLawLine] without this file knowing the brief.
const talkRecipeLaw = "The recipe is the team's. A stage marked fixed is not yours to change; say so if the item needs it."

// talkLawLine is the recipe law as one line of the brief.
func talkLawLine() string { return talkRecipeLaw }
