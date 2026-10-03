package golang

import (
	"reflect"
	"testing"
)

func TestAvailableReleasesFiltersAndSorts(t *testing.T) {
	input := []release{
		{Version: "go1.9.9", Stable: true, Files: []releaseFile{archive("go1.9.9", "amd64")}},
		{Version: "go1.24.1", Stable: true, Files: []releaseFile{archive("go1.24.1", "amd64")}},
		{Version: "go1.24rc1", Stable: false, Files: []releaseFile{archive("go1.24rc1", "amd64")}},
		{Version: "go1.23.8", Stable: true, Files: []releaseFile{archive("go1.23.8", "arm64")}},
		{Version: "go1.24.1", Stable: true, Files: []releaseFile{archive("go1.24.1", "amd64")}},
	}

	got := availableReleases(input, "amd64")
	versions := make([]string, len(got))
	for i := range got {
		versions[i] = got[i].Version
	}
	want := []string{"go1.24.1", "go1.9.9"}
	if !reflect.DeepEqual(versions, want) {
		t.Fatalf("available versions = %#v, want %#v", versions, want)
	}
}

func TestVersionValidationAndComparison(t *testing.T) {
	valid := []string{"go1.22", "go1.22.0", "go1.100.12"}
	for _, version := range valid {
		if !validVersion(version) {
			t.Fatalf("expected %q to be valid", version)
		}
	}
	invalid := []string{"1.22.1", "go1", "go1.22rc1", "go1.02.1", "go1.2.3.4", "go1.x.1"}
	for _, version := range invalid {
		if validVersion(version) {
			t.Fatalf("expected %q to be invalid", version)
		}
	}
	if compareVersions("go1.24.1", "go1.9.10") <= 0 {
		t.Fatal("semantic comparison did not order go1.24.1 after go1.9.10")
	}
	if compareVersions("go1.22", "go1.22.0") != 0 {
		t.Fatal("minor version should compare equal to zero patch version")
	}
}

func archive(version, arch string) releaseFile {
	return releaseFile{
		Filename: version + ".linux-" + arch + ".tar.gz",
		OS:       "linux",
		Arch:     arch,
		Version:  version,
		SHA256:   "abc",
		Kind:     "archive",
	}
}
