package unattended

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/system/packagemanager"
	"snail_tool/internal/ui"
)

func TestParseWhitelistKeepsOrderAndStrictFlag(t *testing.T) {
	state := parseWhitelist(`
Unattended-Upgrade::Package-Whitelist "";
Unattended-Upgrade::Package-Whitelist:: "bash";
Unattended-Upgrade::Package-Whitelist:: "foo\\.bar";
Unattended-Upgrade::Package-Whitelist:: "bash";
Unattended-Upgrade::Package-Whitelist-Strict "true";
`)
	if !state.explicit || !state.strict || strings.Join(state.patterns, ",") != `bash,foo\\.bar` {
		t.Fatalf("parsed whitelist = %+v", state)
	}
	unset := parseWhitelist("Unattended-Upgrade::Package-Whitelist \"\";\n")
	if unset.explicit || unset.strict || len(unset.patterns) != 0 || unset.strictLabel() != "关闭（默认）" || unset.menuStatus() != "按更新来源和排除规则" {
		t.Fatalf("empty whitelist = %+v (%s)", unset, unset.menuStatus())
	}
	if got := unset.clearHint(); got != "清空后按更新来源和排除规则筛选" {
		t.Fatalf("clear hint with strict matching off = %q", got)
	}
	emptyStrict := whitelistState{strict: true}
	if got := emptyStrict.menuStatus(); got != "空名单 · 严格匹配" {
		t.Fatalf("empty strict whitelist status = %q", got)
	}
}

func TestNormalizeWhitelistPatternRejectsQuotesAndBlankInput(t *testing.T) {
	pattern, err := normalizeWhitelistPattern("  ^nginx$  ")
	if err != nil || pattern != "^nginx$" {
		t.Fatalf("normalize = %q, %v", pattern, err)
	}
	if _, err := normalizeWhitelistPattern("foo\"bar"); err == nil {
		t.Fatal("quote was accepted")
	}
	if _, err := normalizeWhitelistPattern("   "); err == nil {
		t.Fatal("blank pattern was accepted")
	}
}

func TestAddWhitelistRejectsInvalidPythonPatternBeforeSaving(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is unavailable")
	}
	service := manager{
		path:  filepath.Join(t.TempDir(), "policy.conf"),
		root:  func() bool { return true },
		query: packagemanager.QueryOutput,
	}
	if err := service.validateWhitelistPattern(`nginx(?=-|$)`); err != nil {
		t.Fatalf("valid Python lookahead was rejected: %v", err)
	}

	input, err := os.CreateTemp(t.TempDir(), "invalid-pattern")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString("nginx(\ny\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previousInput := os.Stdin
	os.Stdin = input
	view := ui.New()
	os.Stdin = previousInput
	err = service.addWhitelistPattern(view, whitelistState{}, nil, false)
	if err == nil || !strings.Contains(err.Error(), "Python 正则无效") {
		t.Fatalf("adding invalid pattern: %v", err)
	}
	if _, err := os.Stat(service.whitelistPath()); !os.IsNotExist(err) {
		t.Fatalf("invalid pattern reached configuration write: %v", err)
	}
	if next, err := view.Ask("检查下一条输入"); err != nil || next != "y" {
		t.Fatalf("invalid pattern reached confirmation: next=%q, err=%v", next, err)
	}
}

func TestWhitelistMenuShowsEffectivePatterns(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("NO_COLOR", "1")
	input, err := os.CreateTemp(t.TempDir(), "whitelist-input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString("11\n1\nq\nq\nq\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previousInput := os.Stdin
	os.Stdin = input
	view := ui.New()
	os.Stdin = previousInput
	output, err := os.CreateTemp(t.TempDir(), "whitelist-output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	service := manager{
		path: filepath.Join(t.TempDir(), "policy.conf"),
		root: func() bool { return true },
		query: func(_ string, args ...string) (string, error) {
			if args[0] == "dump" {
				return "Unattended-Upgrade::Package-Whitelist:: \"bash\";\nUnattended-Upgrade::Package-Whitelist:: \"^nginx$\";\nUnattended-Upgrade::Package-Whitelist-Strict \"true\";\n", nil
			}
			return "PREFERRED='1'\n", nil
		},
	}
	previousOutput := os.Stdout
	os.Stdout = output
	err = service.optionMenu(view)
	os.Stdout = previousOutput
	if !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatalf("option menu did not return: %v", err)
	}
	text, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"软件包允许名单", "2 条 · 严格匹配", "添加规则", "删除规则", "清空名单", "严格匹配",
		"清空后保留严格匹配，通常会阻止自动安装更新",
		"规则 1：bash", "规则 2：^nginx$", "只有名单中的软件包会更新",
		"规则说明", "Python 正则，从软件包名开头匹配", "不必写 ^",
		"前缀：nginx", "精确名称：nginx$", "任意后缀：nginx.*",
		"多选其一：(openssh-server|openssh-client)$", `转义：libstdc\+\+6$`,
		"不能包含双引号，最长 256 字节",
	} {
		if !strings.Contains(string(text), want) {
			t.Errorf("whitelist menu missing %q", want)
		}
	}
	if strings.Contains(string(text), "1   开启") && strings.Contains(string(text), "严格匹配允许名单") {
		t.Fatal("strict whitelist is still a plain on/off toggle")
	}
}

func TestSaveWhitelistReplacesEarlierList(t *testing.T) {
	aptConfig, err := exec.LookPath("apt-config")
	if err != nil {
		t.Skip("apt-config is unavailable")
	}
	dir := t.TempDir()
	parts := filepath.Join(dir, "apt.conf.d")
	if err := os.Mkdir(parts, 0755); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(dir, "apt.conf")
	startup := filepath.Join(dir, "test.conf")
	if err := os.WriteFile(startup, []byte("Dir::Etc::Parts \""+parts+"\";\nDir::Etc::Main \""+mainConfig+"\";\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APT_CONFIG", startup)
	if err := os.WriteFile(filepath.Join(parts, "50unattended-upgrades"), []byte(`
Unattended-Upgrade::Package-Whitelist { "old"; "bash"; };
Unattended-Upgrade::Package-Whitelist-Strict "true";
`), 0644); err != nil {
		t.Fatal(err)
	}
	service := manager{
		path: filepath.Join(parts, filepath.Base(configPath)),
		root: func() bool { return true },
		query: func(_ string, args ...string) (string, error) {
			output, err := exec.Command(aptConfig, args...).CombinedOutput()
			if err != nil {
				return "", err
			}
			return string(output), nil
		},
	}
	load := func() whitelistState {
		t.Helper()
		state, _, _, err := service.loadWhitelist()
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := load()
	if strings.Join(before.patterns, ",") != "old,bash" || !before.strict || before.managed {
		t.Fatalf("initial whitelist = %+v", before)
	}
	if err := service.saveWhitelist([]string{"nginx", `foo\.bar`}, false, nil, false); err != nil {
		t.Fatal(err)
	}
	saved := load()
	if strings.Join(saved.patterns, ",") != `nginx,foo\.bar` || saved.strict || !saved.explicit || !saved.managed {
		t.Fatalf("saved whitelist = %+v", saved)
	}
	_, previous, exists, err := service.loadWhitelist()
	if err != nil || !exists {
		t.Fatal(err)
	}
	if err := service.saveWhitelist(nil, false, previous, exists); err != nil {
		t.Fatal(err)
	}
	cleared := load()
	if len(cleared.patterns) != 0 || cleared.strict || !cleared.managed {
		t.Fatalf("cleared whitelist = %+v", cleared)
	}
	if err := publish(service.whitelistPath(), mustRead(t, service.whitelistPath()), true, "", service.validateAPT); err != nil {
		t.Fatal(err)
	}
	restored := load()
	if strings.Join(restored.patterns, ",") != "old,bash" || !restored.strict || restored.managed {
		t.Fatalf("restored whitelist = %+v", restored)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
