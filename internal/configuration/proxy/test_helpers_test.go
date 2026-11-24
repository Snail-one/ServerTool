package proxy

import (
	"os"
	"testing"
)

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, name := range proxyEnvNames {
		t.Setenv(name, "")
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
