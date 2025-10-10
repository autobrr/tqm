package hardlinkfilemap

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// FileID represents a unique file identifier (device ID + inode number).
type FileID struct {
	Device uint64 // Device ID
	Inode  uint64 // Inode number
}

// String returns a string representation of the FileID.
func (f FileID) String() string {
	return fmt.Sprintf("%d:%d", f.Device, f.Inode)
}

// Equal checks if two FileIDs are equal.
func (f FileID) Equal(other FileID) bool {
	return f.Device == other.Device && f.Inode == other.Inode
}

type HardlinkFileMap struct {
	// hardlinkFileMap maps FileID to a slice of file paths that share that inode
	hardlinkFileMap    map[FileID][]string
	log                *logrus.Entry
	torrentPathMapping map[string]string
}
