//go:build !windows

package fullverification

import (
	"reflect"
	"strings"
	"testing"
)

// A CMAKE PROJECT WITH NOTHING ELSE TO SAY IS GIVEN CMAKE'S OWN BUILD AND TEST.
//
// The shape is a header-only library: a root CMakeLists.txt, a test folder,
// and no CI, script or README command discovery recognizes. CMakeLists.txt
// makes it accountable, so both demands apply; before the CMake default there
// was nothing to meet either with, and every run on it failed verification
// with "no build entrypoint could be discovered".
func TestACMakeProjectIsGivenCMakesOwnBuildAndTest(t *testing.T) {
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\nproject(demo CXX)\ninclude(CTest)\n")
	writeDiscoveryFile(t, workspace, "extras/tests/CMakeLists.txt", "add_test(NAME demo COMMAND true)\n")
	plan := Discover(workspace)
	if !plan.BuildExpected || !plan.TestExpected {
		t.Fatalf("plan = %#v, want a CMake project held to both demands", plan)
	}
	const build = "cmake -S . -B .senior-dev/cmake-build && cmake --build .senior-dev/cmake-build --parallel \"$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 2)\""
	want := []Entrypoint{
		{Kind: KindBuild, Command: build, Source: "CMakeLists.txt"},
		{Kind: KindTest, Command: build + " && cd .senior-dev/cmake-build && ctest --output-on-failure", Source: "CMakeLists.txt"},
	}
	if !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("plan = %#v, want %#v", plan.Entrypoints, want)
	}
}

// THE CMAKE TEST STANDS ALONE. Discover picks each kind on its own, so a CMake
// project whose README or Makefile names a build and no test is paired with
// that build and the CMake test. A test that assumed the CMake build had run
// would fail on a folder nothing configured and report the project's tests
// red; the test default configures and builds its own tree first.
func TestTheCMakeTestBuildsItsOwnTreeWhenTheBuildCameFromElsewhere(t *testing.T) {
	for _, fixture := range []struct {
		name, file, body string
		build            Entrypoint
	}{
		{"README build", "README.md", "Build with `cmake --build build`.\n",
			Entrypoint{Kind: KindBuild, Command: "cmake --build build", Source: "README.md"}},
		{"Makefile build target", "Makefile", "build:\n\tcmake --build out\n",
			Entrypoint{Kind: KindBuild, Command: "make build", Source: "Makefile#build"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeDiscoveryFile(t, workspace, "CMakeLists.txt", "project(demo CXX)\ninclude(CTest)\n")
			writeDiscoveryFile(t, workspace, fixture.file, fixture.body)
			want := []Entrypoint{fixture.build, {
				Kind:    KindTest,
				Command: cmakeConfigureAndBuild + " && cd " + cmakeBuildDirectory + " && ctest --output-on-failure",
				Source:  "CMakeLists.txt",
			}}
			if plan := Discover(workspace); !reflect.DeepEqual(plan.Entrypoints, want) {
				t.Fatalf("plan = %#v, want %#v", plan.Entrypoints, want)
			}
		})
	}
}

// The CMake default is the LAST ecosystem asked, so a project that already
// had a default keeps exactly the one it had, and a CMake project whose own
// files name a command keeps that command.
func TestTheCMakeDefaultNeverDisplacesAnEarlierChoice(t *testing.T) {
	goWorkspace := t.TempDir()
	writeDiscoveryFile(t, goWorkspace, "go.mod", "module example.test/demo\n")
	writeDiscoveryFile(t, goWorkspace, "CMakeLists.txt", "project(demo C)\n")
	plan := Discover(goWorkspace)
	for _, entrypoint := range plan.Entrypoints {
		if entrypoint.Source == "CMakeLists.txt" {
			t.Fatalf("a Go module with a CMakeLists.txt lost its Go default: %#v", plan.Entrypoints)
		}
	}

	pythonWorkspace := t.TempDir()
	writeDiscoveryFile(t, pythonWorkspace, "pyproject.toml", "[project]\nname = \"demo\"\n")
	writeDiscoveryFile(t, pythonWorkspace, "pytest.ini", "[pytest]\n")
	writeDiscoveryFile(t, pythonWorkspace, "tests/test_one.py", "")
	writeDiscoveryFile(t, pythonWorkspace, "CMakeLists.txt", "project(ext C)\n")
	plan = Discover(pythonWorkspace)
	for _, entrypoint := range plan.Entrypoints {
		if entrypoint.Source == "CMakeLists.txt" {
			t.Fatalf("a Python project with a native extension lost its pytest default: %#v", plan.Entrypoints)
		}
	}

	documented := t.TempDir()
	writeDiscoveryFile(t, documented, "CMakeLists.txt", "project(demo CXX)\n")
	writeDiscoveryFile(t, documented, "Makefile", "build:\n\tcmake --build out\ntest:\n\tcd out && ctest\n")
	plan = Discover(documented)
	want := []Entrypoint{
		{Kind: KindBuild, Command: "make build", Source: "Makefile#build"},
		{Kind: KindTest, Command: "make test", Source: "Makefile#test"},
	}
	if !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("plan = %#v, want the Makefile's own targets kept over the CMake default", plan.Entrypoints)
	}
}

func TestADeclaredCommandReplacesDiscoveryForItsKindOnly(t *testing.T) {
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "go.mod", "module example.test/demo\n")
	plan := Discover(workspace).Declare(Declared{Test: "  /scripts/validate.py --poc /tmp/poc  "})
	want := []Entrypoint{
		{Kind: KindBuild, Command: "go build ./...", Source: "go.mod"},
		{Kind: KindTest, Command: "/scripts/validate.py --poc /tmp/poc", Source: DeclaredTestSource},
	}
	if !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("plan = %#v, want %#v", plan.Entrypoints, want)
	}
	if !plan.Entrypoints[1].IsDeclared() || plan.Entrypoints[0].IsDeclared() {
		t.Fatalf("IsDeclared must name exactly the declared entrypoint: %#v", plan.Entrypoints)
	}
}

// A DECLARED KIND IS A DEMAND, AND DECLARING ONE NEVER EXCUSES THE OTHER. A
// workspace discovery called unaccountable is held to what the person named,
// and nothing more; an accountable one still owes the kind they did not name.
func TestADeclaredKindIsDemandedAndTheOtherIsLeftToDiscovery(t *testing.T) {
	bare := t.TempDir()
	writeDiscoveryFile(t, bare, "NOTES.txt", "nothing to build\n")
	plan := Discover(bare).Declare(Declared{Test: "./check.sh"})
	if !plan.TestExpected || plan.BuildExpected {
		t.Fatalf("plan = %#v, want the declared test demanded and no build invented", plan)
	}

	accountable := t.TempDir()
	writeDiscoveryFile(t, accountable, "Makefile", "lint:\n\techo lint\n")
	plan = Discover(accountable).Declare(Declared{Test: "./check.sh"})
	if !plan.BuildExpected {
		t.Fatalf("plan = %#v, want the undeclared build still demanded", plan)
	}
	for _, entrypoint := range plan.Entrypoints {
		if entrypoint.Kind == KindBuild {
			t.Fatalf("plan = %#v, want no build entrypoint in this fixture", plan)
		}
	}
}

func TestDeclaringNothingLeavesThePlanAsDiscovered(t *testing.T) {
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "go.mod", "module example.test/demo\n")
	discovered := Discover(workspace)
	if declared := discovered.Declare(Declared{Build: " ", Test: "\n"}); !reflect.DeepEqual(declared, discovered) {
		t.Fatalf("blank declarations changed the plan: %#v, want %#v", declared, discovered)
	}
}

// The CMake build tree must be senior-dev's own folder, never a directory the
// project could ship. The app package pins the same against its own constant.
func TestTheCMakeBuildTreeIsInsideSeniorDevsOwnFolder(t *testing.T) {
	if !strings.HasPrefix(cmakeBuildDirectory, ".senior-dev/") {
		t.Fatalf("cmakeBuildDirectory = %q, want it under .senior-dev/", cmakeBuildDirectory)
	}
}
