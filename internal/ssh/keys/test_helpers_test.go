package keys

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
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

func generateTestPublicKey(t *testing.T) string {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath, "-C", "test@example")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}
	data, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}
