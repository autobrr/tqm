package hardlinkfilemap

import (
	"fmt"
	"syscall"
)

// getFileID returns the unique file identifier (device + inode) and link count for a file.
// This uses direct syscall.Stat() instead of os.Stat() for better performance.
func getFileID(path string) (FileID, uint64, error) {
	var stat syscall.Stat_t
	err := syscall.Stat(path, &stat)
	if err != nil {
		return FileID{}, 0, fmt.Errorf("stat file: %w", err)
	}

	return FileID{
		Device: stat.Dev,
		Inode:  stat.Ino,
	}, stat.Nlink, nil
}
