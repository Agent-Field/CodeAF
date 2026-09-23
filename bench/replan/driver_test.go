package main

// The driver's own tests. None of them runs a model, the door or the go
// toolchain: they prove the plan the dry run prints is the plan the live run
// would make, that the two arms differ by the one switch and nothing else,
// that every cell has a fixture a stranger can run, and that the readers read
// the shapes the engine writes.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestDryRunPrintsEveryInvocation(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run([]string{"-dry-run", "-out", t.TempDir()}, &out, &errOut); err != nil {
		t.Fatalf("dry run: %v (%s)", err, errOut.String())
	}
	text := out.String()
	want := len(allCells()) * len(allArms()) * 2
	if last := fmt.Sprintf("[%02d/%02d]", want, want); !strings.Contains(text, last) {
		t.Fatalf("the dry run did not print %d invocations:\n%s", want, text)
	}
	for _, c := range allCells() {
		for _, a := range allArms() {
			for _, r := range []string{"r1", "r2"} {
				label := a.Name + "-" + c.id + "-" + r
				if !strings.Contains(text, label) {
					t.Errorf("the dry run never names %s", label)
				}
			}
		}
		if !strings.Contains(text, strings.SplitN(c.brief, "\n", 2)[0]) {
			t.Errorf("the dry run does not carry cell %s's brief", c.id)
		}
	}
	for _, line := range []string{
		rootPlanSeatVar + "=off",
		rootPlanSeatVar + "=1",
		beltVar + "=bash",
		telemetryVar + "=off",
		"-model " + defaultWorkModel,
		"-plan-model " + defaultPlanModel,
	} {
		if !strings.Contains(text, line) {
			t.Errorf("the dry run never says %q", line)
		}
	}
	if strings.Contains(strings.ToLower(text), "api_key") || strings.Contains(text, "OPENROUTER_API_KEY=") {
		t.Error("the dry run names a key")
	}
}

func TestArmsDifferOnlyInTheSwitch(t *testing.T) {
	o := options{replicates: 2, model: defaultWorkModel, planModel: defaultPlanModel, wall: defaultWall, bin: "bin/codeaf"}
	p := composePlan(allCells(), allArms(), o, "/out")
	byCellRep := map[string][]invocation{}
	for _, iv := range p.Invocations {
		key := iv.Cell.id + "/" + strconv.Itoa(iv.Replicate)
		byCellRep[key] = append(byCellRep[key], iv)
	}
	if len(byCellRep) != len(allCells())*2 {
		t.Fatalf("the grid has %d cell-replicates, want %d", len(byCellRep), len(allCells())*2)
	}
	for key, pair := range byCellRep {
		if len(pair) != 2 {
			t.Fatalf("%s has %d arms, want 2", key, len(pair))
		}
		a, b := pair[0], pair[1]
		if a.specFingerprint() != b.specFingerprint() {
			t.Errorf("%s: the arms do not share model, wall and brief", key)
		}
		differ := map[string]bool{}
		envA, envB := envMap(a.armEnv()), envMap(b.armEnv())
		for name := range envA {
			if envA[name] != envB[name] {
				differ[name] = true
			}
		}
		for _, name := range []string{homeVar, "HOME"} {
			delete(differ, name) // each invocation's own throwaway home
		}
		if !reflect.DeepEqual(differ, map[string]bool{rootPlanSeatVar: true}) {
			t.Errorf("%s: the arms differ in %v, want only %s", key, differ, rootPlanSeatVar)
		}
		argvA := strings.Join(a.argv(), " ")
		argvB := strings.Join(b.argv(), " ")
		if strings.ReplaceAll(argvA, a.RunDir, "") != strings.ReplaceAll(argvB, b.RunDir, "") {
			t.Errorf("%s: the door's command line differs between arms:\n%s\n%s", key, argvA, argvB)
		}
	}
	// Replicate outer, cell, then arm innermost: the pair runs back to back.
	if p.Invocations[0].Cell.id != p.Invocations[1].Cell.id || p.Invocations[0].Arm.Name == p.Invocations[1].Arm.Name {
		t.Error("the two arms of one cell do not run back to back")
	}
}

func envMap(list []string) map[string]string {
	m := map[string]string{}
	for _, kv := range list {
		name, value, _ := strings.Cut(kv, "=")
		m[name] = value
	}
	return m
}

func TestEveryCellHasAFixtureAStrangerCanRun(t *testing.T) {
	for _, c := range allCells() {
		files, err := pristineFixture(c)
		if err != nil {
			t.Fatalf("cell %s: %v", c.id, err)
		}
		if _, ok := files["go.mod"]; !ok {
			t.Errorf("cell %s has no go.mod", c.id)
		}
		tests := 0
		for name := range files {
			if strings.HasSuffix(name, "_test.go") {
				tests++
			}
		}
		if tests == 0 {
			t.Errorf("cell %s has no tests to grade it by", c.id)
		}
	}
	r1, _ := pristineFixture(cellR1)
	for _, name := range append([]string{"canon.go"}, r1SymptomFiles...) {
		if _, ok := r1[name]; !ok {
			t.Errorf("r1's measure names %s, which the fixture does not carry", name)
		}
	}
}

func TestEditedFilesReadsTheBeltsEditHands(t *testing.T) {
	for command, want := range map[string][]string{
		"codeaf patch canon.go --old 'a' --new 'b'":                   {"canon.go"},
		"cat > money/money.go <<'EOF'\nif a > b {}\nEOF":              {"money.go"},
		"sed -i 's/x/y/' email.go dedup.go":                           {"dedup.go", "email.go"},
		"go test ./... 2>&1 | tail -5":                                nil,
		"sed -n '1,40p' canon.go":                                     nil,
		"cat canon.go > /dev/null":                                    nil,
		"rm -rf legacy && go build ./...":                             {"legacy/"},
		"git rm -r -q legacy/":                                        {"legacy/"},
		"echo x | tee -a notes.txt":                                   nil,
		"python3 - <<'EOF'\nopen('slug.go','w').write(s)\nEOF":        {"slug.go"},
		"cd /tmp/w/fixture && codeaf patch ./export/export.go --old a": {"export.go"},
	} {
		got := editedFiles(command)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("editedFiles(%q) = %v, want %v", command, got, want)
		}
	}
}

func TestChildEnvReplacesTheArmsNamesAndKeepsTheRest(t *testing.T) {
	base := []string{"PATH=/bin", "HOME=/real", "OPENROUTER_API_KEY=kept", beltVar + "=node"}
	got := envMap(childEnv(base, []string{"HOME=/throwaway", beltVar + "=bash"}))
	if got["HOME"] != "/throwaway" || got[beltVar] != "bash" {
		t.Errorf("the arm's values did not win: %v", got)
	}
	if got["PATH"] != "/bin" || got["OPENROUTER_API_KEY"] != "kept" {
		t.Errorf("the driver's own environment was not carried: %v", got)
	}
}

func TestTheThrowawayProfileCarriesNoKey(t *testing.T) {
	iv := invocation{Model: defaultWorkModel, PlanModel: defaultPlanModel, RunDir: t.TempDir()}
	if err := writeProfile(iv); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(iv.homeDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "key") {
		t.Fatalf("the throwaway profile names a key:\n%s", raw)
	}
	for _, model := range []string{defaultWorkModel, defaultPlanModel} {
		if !strings.Contains(string(raw), model) {
			t.Errorf("the throwaway profile does not seat %s", model)
		}
	}
}

func TestTestsChangedNamesAnEditedOrMissingTest(t *testing.T) {
	dir := t.TempDir()
	pristine := fixtureFiles{"a_test.go": []byte("package a\n"), "b_test.go": []byte("package b\n"), "a.go": []byte("package a\n")}
	if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a // changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := testsChanged(dir, pristine); !reflect.DeepEqual(got, []string{"b_test.go"}) {
		t.Errorf("testsChanged = %v, want the missing b_test.go alone", got)
	}
}
