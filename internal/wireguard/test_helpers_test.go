package wireguard

import (
	"os"
	"testing"

	"snail_tool/internal/ui"
)

func restoreDependencies(t *testing.T) {
	t.Helper()
	previousExists, previousRun, previousOutput, previousRoot := commandExists, commandRun, commandOutput, isRoot
	t.Cleanup(func() {
		commandExists, commandRun, commandOutput, isRoot = previousExists, previousRun, previousOutput, previousRoot
	})
}

func newUIWithInput(t *testing.T, input string) *ui.UI {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	previousStdin := os.Stdin
	os.Stdin = reader
	view := ui.New()
	os.Stdin = previousStdin
	t.Cleanup(func() { _ = reader.Close() })
	return view
}
