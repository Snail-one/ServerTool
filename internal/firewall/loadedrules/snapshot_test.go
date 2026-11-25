package loadedrules_test

import (
	"testing"

	"snail_tool/internal/firewall/loadedrules"
)

func TestParseLiveRulesCountsAllTablesAndScopesPolicyToSnail(t *testing.T) {
	output := `{"nftables":[
		{"metainfo":{"version":"1.1.3"}},
		{"table":{"family":"inet","name":"snail"}},
		{"table":{"family":"ip","name":"docker"}},
		{"table":{"family":"ip6","name":"other"}},
		{"chain":{"family":"inet","table":"snail","name":"input","type":"filter","hook":"input","policy":"drop"}},
		{"chain":{"family":"ip","table":"docker","name":"forward","type":"filter","hook":"forward","policy":"accept"}},
		{"chain":{"family":"ip6","table":"other","name":"input","type":"filter","hook":"input","policy":"accept"}},
		{"rule":{"family":"inet","table":"snail","chain":"input","expr":[{"accept":null}]}},
		{"rule":{"family":"ip","table":"docker","chain":"forward","expr":[{"accept":null}]}}
	]}`
	got := loadedrules.Parse(output)
	if got.Err != nil || got.Version != "1.1.3" || got.Tables != 3 || got.Chains != 3 || got.Rules != 2 || !got.SnailLoaded {
		t.Fatalf("incorrect full ruleset summary: %+v", got)
	}
	if got.InputPolicy != "input：默认丢弃（drop）" {
		t.Fatalf("policy must come only from inet snail: %q", got.InputPolicy)
	}
	if !got.InputActive || got.SnailChains != 1 || got.SnailRules != 1 || got.OtherInputChains != 1 {
		t.Fatalf("incorrect active state or own-table counts: %+v", got)
	}
	for _, output := range []string{`{"nftables":[]}`, `{"nftables":[{"table":{"family":"ip","name":"snail"}}]}`} {
		if got := loadedrules.Parse(output); got.Err != nil || got.SnailLoaded {
			t.Fatalf("missing inet snail table misreported: %+v", got)
		}
	}
	for _, output := range []string{`{}`, `{"nftables":null}`, `invalid json`} {
		if got := loadedrules.Parse(output); got.Err == nil {
			t.Fatalf("unreadable status must be an error, not an empty ruleset: %q", output)
		}
	}
}
