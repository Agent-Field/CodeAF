package main

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// factoryFixtureEnv is the one switch that puts something on the factory page
// before an engine stands behind it.
const factoryFixtureEnv = "CODEAF_FACTORY_FIXTURE"

// factorySeam is the factory page's seam for this launch.
//
// NO ENGINE IS WIRED YET, SO THE ZERO SEAM IS THE HONEST ANSWER. A zero
// [factory.Seam] has no Load and the page draws its one dim line saying what
// arrives there, which is the emptiness law and not a gap. Setting
// CODEAF_FACTORY_FIXTURE=1 hands the page a still fixture instead
// ([factory.FixtureSeam]) so the floor can be looked at and driven by hand;
// every verb on that seam is nil, so the fixture offers no act it cannot do.
func factorySeam() factory.Seam {
	// The mock is asked first. It exists only in a build made with
	// -tags factorymock (internal/factory/mock/REMOVING.md); the default build
	// has a stub that always answers false, so the shipped binary carries none
	// of it.
	if seam, ok := factoryMockSeam(); ok {
		return seam
	}
	if strings.TrimSpace(env.Get(factoryFixtureEnv)) == "1" {
		return factory.FixtureSeam(time.Now())
	}
	return factory.Seam{}
}
