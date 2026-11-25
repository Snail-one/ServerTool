package testenv

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/settings"
	autostartconfig "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func (env *Fixture) AutostartProperties(state string) string {
	return fmt.Sprintf("LoadState=loaded\nUnitFileState=%s\nExecStart={ path=/usr/sbin/nft ; argv[]=/usr/sbin/nft -f %s ; ignore_errors=no ; }\n", state, env.ConfigPath)
}

func WithPacketCounters(content string, enabled bool) (string, error) {
	return counters.Replace(content, enabled, settings.ValidateCounterConfig)
}

func AssertNoPacketCounters(t *testing.T, content string) {
	t.Helper()
	tokens, err := configuration.Tokens(content)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		if token.Text == "counter" {
			t.Fatal("statistics are disabled but an active counter remains")
		}
	}
}

func Input(t *testing.T, input string) *ui.UI {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	previous := os.Stdin
	os.Stdin = r
	view := ui.New()
	os.Stdin = previous
	t.Cleanup(func() { _ = r.Close() })
	return view
}

func IsSSHDQuery(name string, args []string) bool {
	return name == sshaccess.SSHDPath && reflect.DeepEqual(args, []string{"-T"})
}

func SSHDConfig(ports ...int) string {
	lines := make([]string, 0, len(ports)*3)
	for _, port := range ports {
		text := strconv.Itoa(port)
		lines = append(lines, "port "+text, "listenaddress 0.0.0.0:"+text, "listenaddress [::]:"+text)
	}
	return strings.Join(lines, "\n")
}

// Model kernel/service state; unexpected commands fail rather than touching the host.
func Original(t *testing.T, boot string) (*Fixture, *string, *bool, *[][]string) {
	t.Helper()
	env := New(t)
	env.PreserveOriginal = env.SaveOriginal
	loaded := false
	var commands [][]string
	env.CommandExists = func(name string) bool { return name == "nft" || name == "systemctl" }
	env.CommandOutput = func(name string, args ...string) (string, error) {
		switch {
		case name == "systemctl" && len(args) > 0 && args[0] == "show":
			return env.AutostartProperties(boot), nil
		case name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}):
			objects := `{"table":{"family":"ip","name":"docker"}}`
			if loaded {
				objects += `,{"table":{"family":"inet","name":"snail"}}`
			}
			return `{"nftables":[` + objects + `]}`, nil
		case name == "nft" && reflect.DeepEqual(args, []string{"delete", "table", "inet", "snail"}):
			commands = append(commands, append([]string{name}, args...))
			loaded = false
			return "", nil
		case name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f":
			return "", nil
		case name == "nft" && reflect.DeepEqual(args, []string{"-f", env.ConfigPath}):
			commands = append(commands, append([]string{name}, args...))
			loaded = true
			return "", nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return "", errors.New("unexpected command")
		}
	}
	env.CommandRun = func(name string, args ...string) error {
		commands = append(commands, append([]string{name}, args...))
		switch {
		case name == "systemctl" && reflect.DeepEqual(args, []string{"disable", autostartconfig.Unit}):
			boot = "disabled"
		case name == "systemctl" && reflect.DeepEqual(args, []string{"disable", "--runtime", autostartconfig.Unit}):
			boot = "disabled"
		case name == "systemctl" && reflect.DeepEqual(args, []string{"enable", autostartconfig.Unit}):
			boot = "enabled"
		case name == "systemctl" && reflect.DeepEqual(args, []string{"enable", "--runtime", autostartconfig.Unit}):
			boot = "enabled-runtime"
		default:
			t.Fatalf("unexpected mutation: %s %v", name, args)
		}
		return nil
	}
	return env, &boot, &loaded, &commands
}

//go:embed testdata/ports_ruleset.json
var portRules string

func PortRules() string {
	return portRules
}

func CaptureOutput(t *testing.T, run func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "output-*")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = previous; _ = file.Close() }()
	run()
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (env *Fixture) BackupDir() string {
	dir, _ := (storage.BackupStore{Root: env.DataDir, Module: "firewall"}).Directory(env.ConfigPath)
	return dir
}

func (env *Fixture) RuleConfig(t *testing.T, rules []rulespkg.Rule) configuration.Version {
	t.Helper()
	base, err := generate.Build([]int{2222}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := rulespkg.ReplaceManagedRules(base, rules)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.ConfigPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	version, err := env.Store().Read()
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func OrderRules() []rulespkg.Rule {
	return []rulespkg.Rule{
		{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "both"},
		{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.1.0/24", Ports: "443", Protocol: "tcp"},
		{ID: "cccccccc", Kind: "port", Ports: "53", Protocol: "udp"},
		{ID: "dddddddd", Kind: "port", Ports: "8080", Protocol: "tcp"},
	}
}

func (env *Fixture) AcceptRuleCommands(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		if len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
			calls = append(calls, "check")
			if _, err := rulespkg.ManagedRules(storage.ReadFileString(args[2])); err != nil {
				t.Fatalf("invalid rule block: %v", err)
			}
			return "", nil
		}
		if reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
			calls = append(calls, "load")
			return "", nil
		}
		t.Fatalf("unexpected nft arguments: %v", args)
		return "", nil
	}
	return &calls
}

func FieldValue(fields []ui.CardField, label string) string {
	for _, field := range fields {
		if field.Label == label {
			return field.Value
		}
	}
	return ""
}

func (env *Fixture) EditPath(t *testing.T, args []string) string {
	t.Helper()
	if len(args) != 1 || args[0] == env.ConfigPath || filepath.Dir(args[0]) != filepath.Join(env.DataDir, storage.DataTempDirName) ||
		!strings.HasPrefix(filepath.Base(args[0]), ".snail-nftables-edit-") {
		t.Fatalf("expected an editing copy in the ServerTool temporary directory, got %v", args)
	}
	return args[0]
}

const ActiveSSHRules = `{"nftables":[
 {"table":{"family":"inet","name":"snail"}},
 {"chain":{"family":"inet","table":"snail","name":"input","type":"filter","hook":"input","policy":"drop"}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":40,"comment":"snail:ssh-ports"}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":41,"comment":"snail:ssh-ports"}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":42,"comment":"manual-rule"}},
 {"rule":{"family":"inet","table":"other","chain":"input","handle":43,"comment":"snail:ssh-ports"}}
]}`
