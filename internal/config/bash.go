package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
)

const rootBashrc = `# ~/.bashrc: executed by bash(1) for non-login shells.

# Note: PS1 is set in /etc/profile, and the default umask is defined
# in /etc/login.defs. You should not need this unless you want different
# defaults for root.

 PS1='${debian_chroot:+($debian_chroot)}\u@\h:\w\$ '
 PS1='\[\033[38;5;196m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '


# PS1='\[\033[01;35m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '

# umask 022
# You may uncomment the following lines if you want ` + "`ls'" + ` to be colorized:
 export LS_OPTIONS='--color=auto'
 eval "$(dircolors)"
 alias ls='ls $LS_OPTIONS'
 alias ll='ls $LS_OPTIONS -l'
 alias l='ls $LS_OPTIONS -lA'
#
# Some more alias to avoid making mistakes:
 alias rm='rm -i'
 alias cp='cp -i'
 alias mv='mv -i'
`

func ConfigureBash() error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	bashrc := filepath.Join(account.Home, ".bashrc")
	log.Info("配置 Bash 环境...")
	fmt.Printf("当前用户：%s\n", account.Name)
	fmt.Printf("配置文件：%s\n", bashrc)

	if account.Name == "root" {
		if err := os.WriteFile(bashrc, []byte(rootBashrc), 0644); err != nil {
			return err
		}
	} else {
		if err := ensureFile(bashrc); err != nil {
			return err
		}
		if err := replaceAliases(bashrc); err != nil {
			return err
		}
	}

	if err := system.ChownPath(bashrc, account, false); err != nil {
		return err
	}
	if err := os.Chmod(bashrc, 0644); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("已经修改 ~/.bashrc，新的别名配置如下：")
	fmt.Println("alias ll='ls -l'")
	fmt.Println("alias la='ls -A'")
	fmt.Println("alias l='ls -lah'")
	fmt.Println("Bash 配置完成")
	return nil
}

func replaceAliases(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := string(data)
	replacements := map[string]string{
		`(?m)^[[:space:]]*#?[[:space:]]*alias ll=.*$`: "alias ll='ls -l'",
		`(?m)^[[:space:]]*#?[[:space:]]*alias la=.*$`: "alias la='ls -A'",
		`(?m)^[[:space:]]*#?[[:space:]]*alias l=.*$`:  "alias l='ls -lah'",
	}

	for pattern, value := range replacements {
		re := regexp.MustCompile(pattern)
		if re.MatchString(content) {
			content = re.ReplaceAllString(content, value)
		}
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func ensureFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	return file.Close()
}
