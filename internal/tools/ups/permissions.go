package ups

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"snail_tool/internal/log"
)

func applyManagedFileMetadataIfNeeded(file nutConfigFile) error {
	if !shouldEnforceFileMetadata(file) {
		return nil
	}
	return applyManagedFileMetadata(file)
}

func shouldEnforceFileMetadata(file nutConfigFile) bool {
	return file.path == upsOnBattScript
}

func applyManagedFileMetadata(file nutConfigFile) error {
	if file.mode != 0 {
		if err := os.Chmod(file.path, file.mode); err != nil {
			return err
		}
	}
	if file.owner.user == "" && file.owner.group == "" {
		return nil
	}

	uid, gid, err := lookupFileOwner(file.owner)
	if err != nil {
		return err
	}
	if err := os.Chown(file.path, uid, gid); err != nil {
		return err
	}
	return nil
}

func warnIfNUTConfigPermissionsMismatch() {
	files := []nutConfigFile{
		nutConfFile,
		upsConfFile,
		upsdConfFile,
		upsdUsersFile,
		upsmonConfFile,
		upsschedConfFile,
	}

	for _, file := range files {
		if !filePermissionMatches(file) {
			printNUTConfigFilePermissions(files)
			return
		}
	}
}

func filePermissionMatches(file nutConfigFile) bool {
	info, err := os.Stat(file.path)
	if err != nil || info.IsDir() {
		return false
	}
	if info.Mode().Perm() != file.mode {
		return false
	}
	owner, group := fileOwnerNames(info)
	return owner == file.owner.user && group == file.owner.group
}

func printNUTConfigFilePermissions(files []nutConfigFile) {
	log.Warn("/etc/nut 配置文件权限不是预期的 0640 root:nut，当前权限如下：")
	for _, file := range files {
		permission, err := describeFilePermission(file.path)
		if err != nil {
			fmt.Printf("- %s: %v\n", file.path, err)
			continue
		}
		fmt.Println("- " + permission)
	}
}

func describeFilePermission(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	owner, group := fileOwnerNames(info)
	return fmt.Sprintf("%s %s:%s %s", info.Mode().String(), owner, group, path), nil
}

func fileOwnerNames(info os.FileInfo) (string, string) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "unknown", "unknown"
	}
	return userNameFromID(stat.Uid), groupNameFromID(stat.Gid)
}

func userNameFromID(uid uint32) string {
	id := strconv.FormatUint(uint64(uid), 10)
	account, err := user.LookupId(id)
	if err != nil {
		return id
	}
	return account.Username
}

func groupNameFromID(gid uint32) string {
	id := strconv.FormatUint(uint64(gid), 10)
	group, err := user.LookupGroupId(id)
	if err != nil {
		return id
	}
	return group.Name
}

func lookupFileOwner(owner fileOwner) (int, int, error) {
	uid := -1
	gid := -1

	if owner.user != "" {
		value, err := lookupUserID(owner.user)
		if err != nil {
			return -1, -1, err
		}
		uid = value
	}
	if owner.group != "" {
		value, err := lookupGroupID(owner.group)
		if err != nil {
			return -1, -1, err
		}
		gid = value
	}
	return uid, gid, nil
}

func lookupUserID(name string) (int, error) {
	if name == "root" {
		return 0, nil
	}
	account, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf("查找用户 %q 失败: %w", name, err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return 0, fmt.Errorf("解析用户 %q 的 UID 失败: %w", name, err)
	}
	return uid, nil
}

func lookupGroupID(name string) (int, error) {
	if name == "root" {
		return 0, nil
	}
	group, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("查找用户组 %q 失败，请确认已安装 NUT 并存在 nut 组: %w", name, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return 0, fmt.Errorf("解析用户组 %q 的 GID 失败: %w", name, err)
	}
	return gid, nil
}
