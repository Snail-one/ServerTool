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

func setTrafficQuery(t *testing.T, query func(context.Context, trafficTable) (string, error)) {
	t.Helper()
	previous := queryTrafficRules
	queryTrafficRules = query
	t.Cleanup(func() { queryTrafficRules = previous })
}

func setTrafficTablesQuery(t *testing.T, query func(context.Context) ([]trafficTable, error)) {
	t.Helper()
	previous := queryTrafficTables
	queryTrafficTables = query
	t.Cleanup(func() { queryTrafficTables = previous })
}

func TestTrafficQueryUsesTheRequestedNativeTableCommand(t *testing.T) {
	dir := t.TempDir()
	// A fake nft prints its arguments, so no host firewall is queried or changed.
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte("#!/bin/sh\nprintf '<%s>\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	output, err := queryTrafficRules(context.Background(), defaultTrafficTable)
	if err != nil || output != "<-a>\n<list>\n<table>\n<inet>\n<snail>\n" {
		t.Fatalf("unexpected native query arguments: output=%q err=%v", output, err)
	}
	output, err = queryTrafficRules(context.Background(), trafficTable{family: "ip", name: "filter"})
	if err != nil || output != "<-a>\n<list>\n<table>\n<ip>\n<filter>\n" {
		t.Fatalf("selected family/table was lost: output=%q err=%v", output, err)
	}
	output, err = queryTrafficRules(context.Background(), trafficTable{family: "inet", name: `audit"; flush ruleset; #`})
	if err != nil || output != "<-a>\n<list>\n<table>\n<inet>\n<\"audit\\\"; flush ruleset; #\">\n" {
		t.Fatalf("table name was not quoted as nft data: output=%q err=%v", output, err)
	}
	if _, err := queryTrafficRules(context.Background(), trafficTable{family: "inet; flush ruleset", name: "filter"}); err == nil {
		t.Fatal("invalid family was accepted as command text")
	}
}

func TestTrafficMonitorSwitchesTablesAndDefaultsToProjectTableOnEntry(t *testing.T) {
	setupTest(t)
	var queried []trafficTable
	setTrafficQuery(t, func(_ context.Context, table trafficTable) (string, error) {
		queried = append(queried, table)
		return strings.Replace(trafficRawRules, "table inet snail", "table "+table.label(), 1), nil
	})
	listQueries := 0
	setTrafficTablesQuery(t, func(context.Context) ([]trafficTable, error) {
		listQueries++
		return parseTrafficTables(`{"nftables":[
		 {"metainfo":{"version":"1.1.3"}},
		 {"table":{"family":"ip","name":"filter"}},
		 {"table":{"family":"inet","name":"snail"}},
		 {"table":{"family":"ip6","name":"filter"}},
		 {"table":{"family":"ip","name":"filter"}}
		]}`)
	})
	// Switch to IPv6 filter, cancel a later picker, then Enter selects the
	// project default again. A new monitor entry also starts at the default.
	view := inputUI(t, "t\n3\nt\nq\nt\n\nq\nq\nnext\n")
	output := capturePortOutput(t, func() {
		for i := 0; i < 2; i++ {
			if err := monitorTraffic(view, time.Hour); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatal(err)
			}
		}
	})
	want := []trafficTable{defaultTrafficTable, {family: "ip6", name: "filter"}, {family: "ip6", name: "filter"}, defaultTrafficTable, defaultTrafficTable}
	if !reflect.DeepEqual(queried, want) || listQueries != 3 {
		t.Fatalf("wrong targets or unexpected refreshes: queried=%v listQueries=%d", queried, listQueries)
	}
	for _, text := range []string{"本工具 · 默认", "ip filter", "ip6 filter", "nft -a list table ip6 filter", "table ip6 filter"} {
		if !strings.Contains(output, text) {
			t.Fatalf("missing table selection detail %q", text)
		}
	}
	if next, err := view.ReadLine(); err != nil || next != "next" {
		t.Fatal("table switching consumed another menu response")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatal("table observation created backup/configuration data")
	}
}

func TestTrafficMonitorCanSwitchWhenProjectTableIsAbsent(t *testing.T) {
	setupTest(t)
	var queried []trafficTable
	setTrafficQuery(t, func(_ context.Context, table trafficTable) (string, error) {
		queried = append(queried, table)
		if table == defaultTrafficTable {
			return "", errors.New("table does not exist")
		}
		return "table ip filter { }\n", nil
	})
	setTrafficTablesQuery(t, func(context.Context) ([]trafficTable, error) {
		return []trafficTable{{family: "ip", name: "filter"}}, nil
	})
	view := inputUI(t, "t\n2\nq\nnext\n")
	output := capturePortOutput(t, func() {
		if err := monitorTraffic(view, time.Hour); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !reflect.DeepEqual(queried, []trafficTable{defaultTrafficTable, {family: "ip", name: "filter"}}) || !strings.Contains(output, "未加载") || !strings.Contains(output, "table ip filter { }") {
		t.Fatalf("missing default prevented switching: queried=%v output=%s", queried, output)
	}
	if next, err := view.ReadLine(); err != nil || next != "next" {
		t.Fatal("table selection consumed extra input")
	}
}

func TestTrafficMonitorMenuReturnsWithoutPauseOrConfigurationWrites(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	queries := 0
	setTrafficQuery(t, func(context.Context, trafficTable) (string, error) {
		queries++
		return trafficRawRules, nil
	})
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("unexpected mutation or query: %s %v", name, args)
		}
		return `{"nftables":[]}`, nil
	}
	view := inputUI(t, "8\nq\nq\nnext\n")
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
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	_ = os.Unsetenv("NO_COLOR")
	queries := 0
	blocked, cancelled := make(chan struct{}), make(chan struct{})
	setTrafficQuery(t, func(ctx context.Context, _ trafficTable) (string, error) {
		queries++
		switch queries {
		case 1:
			return "", errors.New("temporary read failure")
		case 2:
			return trafficRawRules, nil
		case 3:
			return strings.Replace(trafficRawRules, "packets 27 bytes 2376", "packets 28 bytes 2464", 1), nil
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
	if queries != 4 || !strings.Contains(output, "规则读取失败") || !strings.Contains(output, trafficRawRules) {
		t.Fatalf("query failure did not recover: queries=%d output=%s", queries, output)
	}
	if !strings.Contains(output, "counter packets 2"+ui.PrimaryBoldText("8 bytes 2464")+" drop # handle 65") {
		t.Fatal("updated native counters were not highlighted")
	}
	if next, err := view.ReadLine(); err != nil || next != "next" {
		t.Fatal("watcher stole the following menu input")
	}
}
