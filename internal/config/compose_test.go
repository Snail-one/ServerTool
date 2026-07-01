package config

import (
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

func TestDockerCleanupPlanForChoice(t *testing.T) {
	tests := []struct {
		choice      string
		wantArgs    []string
		wantConfirm bool
		wantSkip    bool
		wantErrPart string
	}{
		{choice: "", wantArgs: []string{"image", "prune", "-f"}},
		{choice: "1", wantArgs: []string{"image", "prune", "-f"}},
		{choice: "2", wantArgs: []string{"image", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "3", wantArgs: []string{"system", "prune", "-f"}},
		{choice: "4", wantArgs: []string{"system", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "q", wantSkip: true},
		{choice: "bad", wantErrPart: "无效 Docker 清理选项"},
	}

	for _, tt := range tests {
		t.Run(tt.choice, func(t *testing.T) {
			got, err := dockerCleanupPlanForChoice(tt.choice)
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrPart, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.args, tt.wantArgs) {
				t.Fatalf("args mismatch: got %#v, want %#v", got.args, tt.wantArgs)
			}
			if got.needsConfirm != tt.wantConfirm {
				t.Fatalf("needsConfirm = %v, want %v", got.needsConfirm, tt.wantConfirm)
			}
			if got.skip != tt.wantSkip {
				t.Fatalf("skip = %v, want %v", got.skip, tt.wantSkip)
			}
		})
	}
}

func TestComposePullArgs(t *testing.T) {
	want := []string{"pull"}
	if got := composePullArgs(composeCommand{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("pull args mismatch: got %#v, want %#v", got, want)
	}
}

func TestComposeUpArgs(t *testing.T) {
	if got, want := composeUpArgs(), []string{"up", "-d", "--remove-orphans"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("up args mismatch: got %#v, want %#v", got, want)
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
