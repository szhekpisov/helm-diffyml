package helmclient

import "os"

func writeFileImpl(path string, data []byte, perm uint32) error {
	return os.WriteFile(path, data, os.FileMode(perm))
}
