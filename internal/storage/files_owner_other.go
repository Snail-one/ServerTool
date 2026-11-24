//go:build !unix

package storage

import "os"

func fileOwner(os.FileInfo) (*FileOwner, error) {
	return nil, nil
}
