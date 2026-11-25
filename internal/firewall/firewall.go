package firewall

import (
	"snail_tool/internal/firewall/internal/manager"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	return manager.DefaultEnvironment().Run(view)
}

func Cleanup(_ *system.Account) error {
	return manager.DefaultEnvironment().Restore().Run()
}

// CleanupWithConfirm is the dedicated action under the configuration cleanup menu.
func CleanupWithConfirm(view *ui.UI) error {
	return manager.DefaultEnvironment().Restore().RunWithConfirm(view)
}

// EnsureSSHPorts keeps firewall access to SSH ports during service reload.
func EnsureSSHPorts(ports []int) (bool, error) {
	return manager.DefaultEnvironment().SSHAccess().EnsureSSHPorts(ports)
}

func SyncDockerForward() error {
	return manager.DefaultEnvironment().DockerMark().Sync()
}

func RemoveDockerForwardService() error {
	return manager.DefaultEnvironment().DockerMark().RemoveService()
}
