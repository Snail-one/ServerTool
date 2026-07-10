package update

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFindComposeDirsMatchesScriptDepthAndDedupes(t *testing.T) {
	root := t.TempDir()
	appOne := filepath.Join(root, "app-one")
	appTwo := filepath.Join(root, "app-two")

	writeTestFile(t, filepath.Join(appOne, "docker-compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(appOne, "compose.yaml"), "services: {}\n")
	writeTestFile(t, filepath.Join(appTwo, "compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(root, "docker-compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(root, "group", "app-three", "docker-compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(root, "app-four", "Dockerfile"), "FROM scratch\n")

	got, err := findComposeDirs(root)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{appOne, appTwo}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compose dirs mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestFindComposeDirsReturnsEmptyWhenNoneFound(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "app", "Dockerfile"), "FROM scratch\n")

	got, err := findComposeDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no compose dirs, got %#v", got)
	}
}

func TestFindComposeDirsInRootsDedupesAcrossRoots(t *testing.T) {
	root := t.TempDir()
	firstRoot := filepath.Join(root, "first")
	secondRoot := filepath.Join(root, "second")
	appOne := filepath.Join(firstRoot, "app-one")
	appTwo := filepath.Join(secondRoot, "app-two")

	writeTestFile(t, filepath.Join(appOne, "compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(appTwo, "docker-compose.yaml"), "services: {}\n")

	got, err := findComposeDirsInRoots([]string{firstRoot, secondRoot, firstRoot})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{appOne, appTwo}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compose dirs mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestParseComposeRootsUsesDefaultsAndSplitsInput(t *testing.T) {
	defaults := []string{"/opt/apps", "/docker"}
	if got := parseComposeRoots("", defaults); !reflect.DeepEqual(got, defaults) {
		t.Fatalf("default roots mismatch: got %#v, want %#v", got, defaults)
	}

	got := parseComposeRoots("/opt/apps, /docker /opt/apps//", defaults)
	want := []string{"/opt/apps", "/docker"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed roots mismatch: got %#v, want %#v", got, want)
	}
}

func TestComposePullArgs(t *testing.T) {
	want := []string{"pull"}
	if got := composePullArgs(ComposeCommand{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("pull args mismatch: got %#v, want %#v", got, want)
	}
}

func TestComposeUpArgs(t *testing.T) {
	if got, want := composeUpArgs(), []string{"up", "-d", "--remove-orphans"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("up args mismatch: got %#v, want %#v", got, want)
	}
}

func TestComposeRebuildArgs(t *testing.T) {
	if got, want := composeRebuildDownArgs(), []string{"down"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuild down args mismatch: got %#v, want %#v", got, want)
	}
	if got, want := composeRebuildUpArgs(), []string{"up", "-d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuild up args mismatch: got %#v, want %#v", got, want)
	}
}

func TestRebuildRunningComposeProjectsNoDirsSkipsCommands(t *testing.T) {
	confirmer := &fakeComposeRebuildConfirmer{confirmed: true}
	runner := &fakeComposeRunner{}

	result, err := rebuildRunningComposeProjects(confirmer, ComposeCommand{Display: "docker compose"}, nil, runner.run, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Rebuilt != 0 || result.Canceled {
		t.Fatalf("unexpected result: %#v", result)
	}
	if confirmer.calls != 0 {
		t.Fatalf("confirm calls = %d, want 0", confirmer.calls)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
	}
}

func TestRebuildRunningComposeProjectsRejectSkipsCommands(t *testing.T) {
	confirmer := &fakeComposeRebuildConfirmer{confirmed: false}
	runner := &fakeComposeRunner{}

	result, err := rebuildRunningComposeProjects(confirmer, ComposeCommand{Display: "docker compose"}, []string{"/srv/app"}, runner.run, "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Canceled || result.Rebuilt != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if confirmer.calls != 1 {
		t.Fatalf("confirm calls = %d, want 1", confirmer.calls)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
	}
}

func TestRebuildRunningComposeProjectsRunsDownThenUpInDirOrder(t *testing.T) {
	confirmer := &fakeComposeRebuildConfirmer{confirmed: true}
	runner := &fakeComposeRunner{}

	result, err := rebuildRunningComposeProjects(
		confirmer,
		ComposeCommand{Name: "docker", Args: []string{"compose"}, Display: "docker compose"},
		[]string{"/srv/app-one", "/srv/app-two"},
		runner.run,
		"confirm? ",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rebuilt != 2 || result.Canceled {
		t.Fatalf("unexpected result: %#v", result)
	}

	want := []composeRunCall{
		{dir: "/srv/app-one", args: []string{"down"}},
		{dir: "/srv/app-one", args: []string{"up", "-d"}},
		{dir: "/srv/app-two", args: []string{"down"}},
		{dir: "/srv/app-two", args: []string{"up", "-d"}},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("runner calls mismatch\ngot:  %#v\nwant: %#v", runner.calls, want)
	}
}

func TestRebuildRunningComposeProjectsStopsOnDownFailure(t *testing.T) {
	confirmer := &fakeComposeRebuildConfirmer{confirmed: true}
	runner := &fakeComposeRunner{failAt: 1, err: errors.New("boom")}

	_, err := rebuildRunningComposeProjects(confirmer, ComposeCommand{Display: "docker compose"}, []string{"/srv/app"}, runner.run, "")
	if err == nil || !strings.Contains(err.Error(), "/srv/app down 失败") {
		t.Fatalf("expected down failure with dir, got %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %#v, want one down call", runner.calls)
	}
}

func TestRebuildRunningComposeProjectsStopsOnUpFailure(t *testing.T) {
	confirmer := &fakeComposeRebuildConfirmer{confirmed: true}
	runner := &fakeComposeRunner{failAt: 2, err: errors.New("boom")}

	_, err := rebuildRunningComposeProjects(confirmer, ComposeCommand{Display: "docker compose"}, []string{"/srv/app"}, runner.run, "")
	if err == nil || !strings.Contains(err.Error(), "/srv/app up -d 失败") {
		t.Fatalf("expected up failure with dir, got %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("runner calls = %#v, want down and up calls", runner.calls)
	}
}

func TestComposeCommandCandidatesFollowRuntimeOrder(t *testing.T) {
	tests := []struct {
		name        string
		runtimeName string
		want        []composeCommandCandidate
	}{
		{
			name:        "docker runtime",
			runtimeName: "docker",
			want: []composeCommandCandidate{
				{name: "docker", args: []string{"compose"}, display: "docker compose"},
				{name: "docker-compose", display: "docker-compose"},
			},
		},
		{
			name:        "podman runtime",
			runtimeName: "podman",
			want: []composeCommandCandidate{
				{name: "podman", args: []string{"compose"}, display: "podman compose"},
				{name: "podman-compose", display: "podman-compose"},
			},
		},
		{
			name: "unknown runtime uses docker first",
			want: []composeCommandCandidate{
				{name: "docker", args: []string{"compose"}, display: "docker compose"},
				{name: "docker-compose", display: "docker-compose"},
				{name: "podman", args: []string{"compose"}, display: "podman compose"},
				{name: "podman-compose", display: "podman-compose"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := composeCommandCandidatesForRuntime(tt.runtimeName)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("compose candidates mismatch\ngot:  %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}

func TestComposeConfigHasBuild(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    bool
		wantErr bool
	}{
		{
			name: "build object",
			raw:  `{"services":{"app":{"build":{"context":"/srv/app"}}}}`,
			want: true,
		},
		{
			name: "build string",
			raw:  `{"services":{"app":{"build":"."}}}`,
			want: true,
		},
		{
			name: "null build",
			raw:  `{"services":{"app":{"image":"nginx","build":null}}}`,
		},
		{
			name: "image only",
			raw:  `{"services":{"app":{"image":"nginx"}}}`,
		},
		{
			name:    "invalid json",
			raw:     `{`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := composeConfigHasBuild([]byte(tt.raw))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("has build = %v, want %v", got, tt.want)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

type fakeComposeRebuildConfirmer struct {
	confirmed bool
	calls     int
}

func (f *fakeComposeRebuildConfirmer) Confirm(string) (bool, error) {
	f.calls++
	return f.confirmed, nil
}

type composeRunCall struct {
	dir  string
	args []string
}

type fakeComposeRunner struct {
	calls  []composeRunCall
	failAt int
	err    error
}

func (f *fakeComposeRunner) run(_ ComposeCommand, dir string, args ...string) error {
	f.calls = append(f.calls, composeRunCall{dir: dir, args: append([]string{}, args...)})
	if f.failAt > 0 && len(f.calls) == f.failAt {
		return f.err
	}
	return nil
}
