//go:build !unix

package shared

import "os"

func fileOwner(os.FileInfo) (*FileOwner, error) {
	return nil, nil
}
