package shared

import "testing"

func TestFormatManagedBlockAddsBlankLinesAroundBody(t *testing.T) {
	got := FormatManagedBlock("# BEGIN", "line one\nline two", "# END")
	want := "# BEGIN\n\nline one\nline two\n\n# END\n"
	if got != want {
		t.Fatalf("FormatManagedBlock() = %q, want %q", got, want)
	}
}
