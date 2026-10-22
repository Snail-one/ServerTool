package firewall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const trafficRawRules = `table inet snail { # handle 9
    chain public_ports { # handle 1
        meta nfproto ipv4 tcp dport 21069 counter packets 0 bytes 0 accept # handle 61
        counter packets 27 bytes 2376 drop # handle 65
    }
    chain private_ports { # handle 2
        counter packets 2097069 bytes 92274688 drop # handle 70
    }
    chain source_allow { # handle 3
        return # handle 73
    }
    chain input { # handle 4
        type filter hook input priority filter; policy drop;
        ct state established,related counter packets 82982 bytes 3863642 accept # handle 74
        jump source_allow # handle 84
        goto public_ports # handle 89
    }
}
`

func setTrafficQuery(t *testing.T, query func(context.Context) (string, error)) {
	t.Helper()
	previous := queryTrafficRules
	queryTrafficRules = query
	t.Cleanup(func() { queryTrafficRules = previous })
}

func TestTrafficQueryUsesTheRequestedNativeTableCommand(t *testing.T) {
	dir := t.TempDir()
	// A fake nft prints its arguments, so no host firewall is queried or changed.
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte("#!/bin/sh\nprintf '<%s>\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	output, err := queryTrafficRules(context.Background())
	if err != nil || output != "<-a>\n<list>\n<table>\n<inet>\n<snail>\n" {
		t.Fatalf("unexpected native query arguments: output=%q err=%v", output, err)
	}
}

func TestTrafficMonitorMenuReturnsWithoutPauseOrConfigurationWrites(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	queries := 0
	setTrafficQuery(t, func(context.Context) (string, error) {
		queries++
		return trafficRawRules, nil
	})
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("unexpected mutation or query: %s %v", name, args)
		}
		return `{"nftables":[]}`, nil
	}
	view := inputUI(t, "9\nq\nq\nnext\n")
	output := capturePortOutput(t, func() {
		if err := Run(view); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if queries != 1 || shared.ReadFileString(configPath) != string(version.content) {
		t.Fatal("monitor queried after exit or changed configuration")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatal("read-only monitor created software data or backup files")
	}
	if next, err := view.Ask("next"); err != nil || next != "next" {
		t.Fatal("return consumed another menu response")
	}
	for _, text := range []string{"实时查看规则", "nft -a list table inet snail", trafficRawRules} {
		if !strings.Contains(output, text) {
			t.Fatalf("missing %q in %s", text, output)
		}
	}
}

func TestTrafficMonitorRetriesAndCancelsItsWorkerBeforeReturning(t *testing.T) {
	setupTest(t)
	queries := 0
	blocked, cancelled := make(chan struct{}), make(chan struct{})
	setTrafficQuery(t, func(ctx context.Context) (string, error) {
		queries++
		switch queries {
		case 1:
			return "", errors.New("temporary read failure")
		case 2:
			return trafficRawRules, nil
		default:
			close(blocked)
			<-ctx.Done()
			close(cancelled)
			return "", ctx.Err()
		}
	})
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	oldStdin := os.Stdin
	os.Stdin = reader
	view := ui.New()
	os.Stdin = oldStdin
	go func() {
		select {
		case <-blocked:
		case <-time.After(2 * time.Second):
		}
		_, _ = writer.WriteString("q\nnext\n")
	}()
	output := capturePortOutput(t, func() {
		if err := monitorTraffic(view, time.Millisecond); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	select {
	case <-cancelled:
	default:
		t.Fatal("return left a reader/display worker running")
	}
	if queries != 3 || !strings.Contains(output, "规则读取失败") || !strings.Contains(output, trafficRawRules) {
		t.Fatalf("query failure did not recover: queries=%d output=%s", queries, output)
	}
	if next, err := view.ReadLine(); err != nil || next != "next" {
		t.Fatal("watcher stole the following menu input")
	}
}
