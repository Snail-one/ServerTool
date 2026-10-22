package firewall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func trafficTestRule(chain string, handle int, expressions string) string {
	return fmt.Sprintf(`{"rule":{"family":"inet","table":"snail","chain":%q,"handle":%d,"expr":[%s]}}`, chain, handle, expressions)
}

func trafficTestRules(rules ...string) string {
	objects := []string{
		`{"table":{"family":"inet","name":"snail"}}`,
		`{"chain":{"family":"inet","table":"snail","name":"input","type":"filter","hook":"input","policy":"drop"}}`,
	}
	return `{"nftables":[` + strings.Join(append(objects, rules...), ",") + `]}`
}

func trafficTestCounter(packets, bytes uint64) string {
	return fmt.Sprintf(`{"counter":{"packets":%d,"bytes":%d}},{"drop":null}`, packets, bytes)
}

func TestTrafficSnapshotCountsDropsButExcludesAcceptsAndOtherTables(t *testing.T) {
	output := trafficTestRules(
		trafficTestRule("public_ports", 70, trafficTestCounter(1871196, 82337184)),
		trafficTestRule("private_ports", 71, trafficTestCounter(25, 1250)),
		trafficTestRule("input", 72, `{"match":{"op":"==","left":{"payload":{"protocol":"icmpv6","field":"type"}},"right":"echo-request"}},`+trafficTestCounter(12, 600)),
		trafficTestRule("input", 73, `{"reject":{"type":"icmpx"}}`),
		trafficTestRule("input", 74, `{"counter":{"packets":82982,"bytes":3863642}},{"accept":null}`),
		`{"rule":{"family":"inet","table":"other","chain":"input","handle":75,"expr":[{"counter":{"packets":999999,"bytes":999999}},{"drop":null}]}}`,
	)
	snapshot, err := parseTrafficSnapshot(output, time.Unix(1, 0))
	if err != nil || len(snapshot.rules) != 4 || snapshot.counted != 3 || snapshot.total.packets != 1871233 || snapshot.total.bytes != 82339034 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot.rules[2].label != "ICMPv6 回显 #72" || snapshot.rules[3].counter != nil || snapshot.rules[3].action != "reject" {
		t.Fatal("ICMP label or missing counter was not distinguished")
	}
	// First sample is a baseline: cumulative totals must not appear as a rate.
	snapshot.compare(nil)
	if snapshot.rateReady || snapshot.packetsRate != 0 {
		t.Fatal("first sample invented a per-second rate")
	}
}

func TestTrafficCountersDoNotOvercountConditionsAfterCounter(t *testing.T) {
	match := `{"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":80}}`
	counter := `{"counter":{"packets":10,"bytes":1000}}`
	for _, scenario := range []struct {
		name, expressions string
		counted           bool
	}{
		{"normal", match + `,` + counter + `,{"drop":null}`, true},
		{"log after counter", counter + `,{"log":{"prefix":"drop"}},{"drop":null}`, true},
		{"match after counter", counter + `,` + match + `,{"drop":null}`, false},
		{"limit after counter", counter + `,{"limit":{"rate":5}},{"drop":null}`, false},
		{"multiple counters", counter + `,` + counter + `,{"drop":null}`, false},
		{"named counter", `{"counter":"shared"},{"drop":null}`, false},
		{"negative count", `{"counter":{"packets":-1,"bytes":100}},{"drop":null}`, false},
		{"no counter", `{"drop":null}`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			snapshot, err := parseTrafficSnapshot(trafficTestRules(trafficTestRule("input", 1, scenario.expressions)), time.Now())
			if err != nil || len(snapshot.rules) != 1 || (snapshot.rules[0].counter != nil) != scenario.counted {
				t.Fatalf("incorrect coverage: %+v %v", snapshot, err)
			}
		})
	}
}

func TestTrafficRatesUseActualIntervalAndSurviveLargeCounters(t *testing.T) {
	const large = uint64(9007199254740993)
	before, err := parseTrafficSnapshot(trafficTestRules(trafficTestRule("input", 1, trafficTestCounter(large, large*4))), time.Unix(10, 0))
	if err != nil || before.total.packets != large {
		t.Fatalf("large integer lost precision: %+v %v", before, err)
	}
	after, err := parseTrafficSnapshot(trafficTestRules(trafficTestRule("input", 1, trafficTestCounter(large+30, large*4+3000))), time.Unix(12, 0))
	if err != nil {
		t.Fatal(err)
	}
	after.compare(&before)
	if !after.rateReady || after.reset || after.packetsRate != 15 || after.bytesRate != 1500 || !after.rules[0].rateReady {
		t.Fatalf("rates used lifetime totals, rounded integers or the wrong interval: %+v", after)
	}
}

func TestTrafficReloadRemovalAndCounterResetNeverUnderflow(t *testing.T) {
	before, _ := parseTrafficSnapshot(trafficTestRules(trafficTestRule("input", 1, trafficTestCounter(1000, 50000))), time.Unix(10, 0))
	for _, rule := range []string{
		trafficTestRule("input", 1, trafficTestCounter(5, 200)),
		trafficTestRule("input", 2, trafficTestCounter(1200, 60000)),
		trafficTestRule("input", 1, strings.Replace(trafficTestCounter(1200, 60000), "drop", "reject", 1)),
		"",
	} {
		var output string
		if rule == "" {
			output = trafficTestRules()
		} else {
			output = trafficTestRules(rule)
		}
		after, err := parseTrafficSnapshot(output, time.Unix(11, 0))
		if err != nil {
			t.Fatal(err)
		}
		after.compare(&before)
		if after.rateReady || !after.reset || after.packetsRate != 0 || after.bytesRate != 0 {
			t.Fatalf("reset or identity change caused an invalid rate: %+v", after)
		}
	}
}

func setTrafficQuery(t *testing.T, query func(context.Context) (string, error)) {
	t.Helper()
	previous := queryTrafficRules
	queryTrafficRules = query
	t.Cleanup(func() { queryTrafficRules = previous })
}

func TestTrafficMonitorMenuReturnsWithoutPauseOrConfigurationWrites(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	queries := 0
	setTrafficQuery(t, func(context.Context) (string, error) {
		queries++
		return trafficTestRules(trafficTestRule("public_ports", 70, trafficTestCounter(10, 1000))), nil
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
	for _, text := range []string{"实时拦截统计", "累计拦截：10 包", "当前速率：采样中", "公网端口 #70"} {
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
			return trafficTestRules(trafficTestRule("input", 1, trafficTestCounter(100, 4000))), nil
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
	if queries != 3 || !strings.Contains(output, "统计读取失败") || !strings.Contains(output, "累计拦截：100 包") {
		t.Fatalf("query failure did not recover: queries=%d output=%s", queries, output)
	}
	if next, err := view.ReadLine(); err != nil || next != "next" {
		t.Fatal("watcher stole the following menu input")
	}
}
