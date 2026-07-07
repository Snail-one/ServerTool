package system

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Account struct {
	Name string
	Home string
	UID  int
	GID  int
}

func IsRoot() bool {
	out, err := exec.Command("id", "-u").Output()
	return err == nil && strings.TrimSpace(string(out)) == "0"
}

func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return err
	}

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	for {
		select {
		case err := <-done:
			return err
		case sig := <-interrupts:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}
}

func Output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func CurrentTargetUser() (*Account, error) {
	name := os.Getenv("SUDO_USER")
	if name == "" {
		name = os.Getenv("USER")
	}
	if name == "" {
		current, err := user.Current()
		if err != nil {
			return nil, err
		}
		name = current.Username
	}
	return LookupAccount(name)
}

func LookupAccount(name string) (*Account, error) {
	out, err := exec.Command("getent", "passwd", name).Output()
	if err != nil {
		return nil, fmt.Errorf("无法获取用户 %s 信息: %w", name, err)
	}

	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(parts) < 7 {
		return nil, fmt.Errorf("用户 %s passwd 信息格式异常", name)
	}

	uid, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, err
	}
	gid, err := strconv.Atoi(parts[3])
	if err != nil {
		return nil, err
	}

	if parts[5] == "" || !DirExists(parts[5]) {
		return nil, fmt.Errorf("无法获取用户 %s 的 home 目录", name)
	}

	return &Account{Name: name, Home: parts[5], UID: uid, GID: gid}, nil
}

func UserInAdminGroup(name string) bool {
	out, err := exec.Command("groups", name).Output()
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`\b(sudo|wheel)\b`)
	return re.MatchString(string(out))
}

func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func FileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func ChownPath(path string, account *Account, recursive bool) error {
	if recursive {
		return filepath.WalkDir(path, func(item string, _ os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chown(item, account.UID, account.GID)
		})
	}
	return os.Chown(path, account.UID, account.GID)
}

func Backup(path string) (string, error) {
	backup := fmt.Sprintf("%s.bak.%s", path, time.Now().Format("20060102_150405"))
	input, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return backup, os.WriteFile(backup, input, 0644)
}

func PortInUse(port int) bool {
	if canBind(port) {
		return false
	}

	if CommandExists("ss") {
		return commandShowsPort("ss", []string{"-Htanl"}, port)
	}
	if CommandExists("netstat") {
		return commandShowsPort("netstat", []string{"-tanl"}, port)
	}
	return false
}

func RandomFreePort() int {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for {
		port := rng.Intn(50000-20000+1) + 20000
		if !PortInUse(port) {
			return port
		}
	}
}

func ValidatePort(raw string) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("端口必须是数字")
	}
	if port < 1 || port > 65535 {
		return 0, errors.New("端口范围必须是 1-65535")
	}
	return port, nil
}

func ValidateSSHPublicKey(pubkey string) error {
	if strings.TrimSpace(pubkey) == "" {
		return errors.New("公钥不能为空")
	}

	tmp, err := os.CreateTemp("", "snail-ssh-key-*.pub")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(pubkey + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if _, err := Output("ssh-keygen", "-l", "-f", tmp.Name()); err != nil {
		return errors.New("无效 SSH 公钥")
	}
	return nil
}

func SystemdUnitExists(unit string) bool {
	out, err := Output("systemctl", "list-unit-files", unit)
	return err == nil && strings.Contains(out, unit)
}

func commandShowsPort(name string, args []string, port int) bool {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return false
	}
	re := regexp.MustCompile(fmt.Sprintf(`(^|\D)%d(\s|$)`, port))
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if re.Match(line) {
			return true
		}
	}
	return false
}

func canBind(port int) bool {
	listener, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}
