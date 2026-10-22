package shared

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// RememberManaged records the exact bytes this tool intends to write. Keep the
// record outside native config formats (Docker rejects unknown JSON options).
// Publish before replacing the config: failure cannot authorize deleting an
// older or manually edited file whose contents differ from the record.
func (store BackupStore) RememberManaged(source string, data []byte) error {
	path, err := store.sourcePath(source)
	if err != nil {
		return err
	}
	dir, err := store.ensureDirectory(source)
	if err != nil {
		return err
	}
	unlock, err := lockBackupDirectory(dir)
	if err != nil {
		return err
	}
	defer unlock()
	hash := sha256.Sum256(data)
	return AtomicWriteFile(path+".managed.sha256", []byte(hex.EncodeToString(hash[:])+"\n"), AtomicWriteOptions{Mode: 0600, ForceMode: true})
}

func (store BackupStore) MatchesManaged(source string, data []byte) (bool, error) {
	path, err := store.sourcePath(source)
	if err != nil {
		return false, err
	}
	record, exists, err := ReadRegularFile(path + ".managed.sha256")
	if err != nil || !exists {
		return false, err
	}
	digest := strings.TrimSpace(string(record))
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return false, fmt.Errorf("配置写入记录损坏，未清理: %s", source)
	}
	hash := sha256.Sum256(data)
	return digest == hex.EncodeToString(hash[:]), nil
}

// Once the owned settings have been removed, do not claim a future manually
// recreated file merely because it happens to equal an older tool template.
func (store BackupStore) ForgetManaged(source string) error {
	path, err := store.sourcePath(source)
	if err != nil {
		return err
	}
	path += ".managed.sha256"
	if _, exists, err := ReadRegularFile(path); err != nil || !exists {
		return err
	}
	return RemoveRegularFile(path)
}
