package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// wallFlag is a hard wall typed the way every other duration in the product is
// typed: with a unit, `5m` or `2h`. It exists because `aforge do` took its
// wall as a bare integer of seconds while `AFORGE_PRACTICE_IDLE 20m`,
// `AFORGE_BRIEF_AFTER 4h` and `--max-hours` all take units, so a person who
// had typed `20m` anywhere else typed `5m` here and was refused with a parse
// error (#376).
//
// A BARE NUMBER IS STILL SECONDS, FOR ONE RELEASE. Every script that passes
// `-timeout 900` keeps working exactly as it did; the grace is written on the
// flag's own help line so nobody has to remember it.
type wallFlag struct{ wall time.Duration }

func (f *wallFlag) String() string { return f.wall.String() }

func (f *wallFlag) Set(text string) error {
	wall, err := parseWall(text)
	if err != nil {
		return err
	}
	f.wall = wall
	return nil
}

// parseWall reads a wall from a person's spelling of it: a duration with a
// unit, or a bare integer of seconds. Zero and below are refused here rather
// than downstream, so the one error a person sees names the flag they typed.
func parseWall(text string) (time.Duration, error) {
	text = strings.TrimSpace(text)
	if seconds, err := strconv.Atoi(text); err == nil {
		return positiveWall(time.Duration(seconds) * time.Second)
	}
	wall, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("a duration such as 15m or 2h, or a number of seconds")
	}
	return positiveWall(wall)
}

func positiveWall(wall time.Duration) (time.Duration, error) {
	if wall <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return wall, nil
}
