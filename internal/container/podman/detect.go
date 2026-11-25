package podman

// Detect reports whether the podman command is on PATH.
func Detect(commandExists func(string) bool) bool {
	return commandExists("podman")
}
