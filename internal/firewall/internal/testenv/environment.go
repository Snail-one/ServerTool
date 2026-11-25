// Package testenv provides isolated command stubs and fixtures for firewall tests.
package testenv

import (
	"errors"
	"path/filepath"
	"testing"

	"snail_tool/internal/firewall/internal/manager"
)

type Fixture struct {
	*manager.Environment
}

func New(t *testing.T) *Fixture {
	t.Helper()
	t.Setenv("SSH_CONNECTION", "")
	return &Fixture{&manager.Environment{
		ConfigPath:            filepath.Join(t.TempDir(), "nftables.conf"),
		DataDir:               filepath.Join(t.TempDir(), "snailtool"),
		ForwardingProcRoot:    t.TempDir(),
		ForwardingConfigPath:  filepath.Join(t.TempDir(), "forwarding.conf"),
		DockerForwardUnitPath: filepath.Join(t.TempDir(), "absent-snail-docker-forward.service"),
		IsRoot:                func() bool { return true },
		CommandExists:         func(name string) bool { return name == "nft" },
		CommandRun: func(name string, args ...string) error {
			t.Fatalf("unexpected mutation: %s %v", name, args)
			return nil
		},
		CommandOutput: func(name string, args ...string) (string, error) {
			t.Fatalf("unexpected query: %s %v", name, args)
			return "", errors.New("unexpected query")
		},
		PreserveOriginal: func() error { return nil },
	}}
}
