package commands

import (
	"snail_tool/internal/system/packagemanager"
)

// Inventory shares one package database snapshot between the tool count,
// menu, and installation actions. Refresh it after an installation or on demand.
type Inventory struct {
	manager   packagemanager.Manager
	installed map[string]bool
	err       error
}

func NewInventory() *Inventory {
	manager, err := packagemanager.Detect(commandExists)
	inventory := &Inventory{manager: manager, err: err}
	if err == nil {
		_ = inventory.Refresh()
	}
	return inventory
}

func (inventory *Inventory) Refresh() error {
	if inventory.manager.Name == "" {
		return inventory.err
	}
	inventory.installed, inventory.err = packagemanager.Installed(inventory.manager, commandOutput)
	return inventory.err
}

func (inventory *Inventory) InstalledCount() (int, int, error) {
	if inventory.err != nil {
		return 0, len(commonTools), inventory.err
	}
	installed := 0
	for _, tool := range commonTools {
		if inventory.toolInstalled(tool) {
			installed++
		}
	}
	return installed, len(commonTools), nil
}

func (inventory *Inventory) toolInstalled(tool commandLineTool) bool {
	if inventory.err != nil {
		return false
	}
	for _, name := range tool.packageNames(inventory.manager) {
		if !inventory.installed[name] {
			return false
		}
	}
	return true
}
