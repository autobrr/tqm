package hardlinkfilemap

import (
	"fmt"
	"os"
	"reflect"
	"syscall"
)

// getFileID returns the unique file identifier (device + inode) and link count for a file on Windows.
// This uses direct syscall instead of os.Stat() for better performance.
func getFileID(path string) (FileID, uint64, error) {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return FileID{}, 0, fmt.Errorf("convert path to UTF16: %w", err)
	}

	// Check if it's a symlink
	fi, err := os.Lstat(path)
	if err != nil {
		return FileID{}, 0, fmt.Errorf("lstat file: %w", err)
	}

	attrs := uint32(syscall.FILE_FLAG_BACKUP_SEMANTICS)
	if isSymlink(fi) {
		// Use FILE_FLAG_OPEN_REPARSE_POINT, otherwise CreateFile will follow symlink.
		// See https://docs.microsoft.com/en-us/windows/desktop/FileIO/symbolic-link-effects-on-file-systems-functions#createfile-and-createfiletransacted
		attrs |= syscall.FILE_FLAG_OPEN_REPARSE_POINT
	}

	h, err := syscall.CreateFile(pathp, 0, 0, nil, syscall.OPEN_EXISTING, attrs, 0)
	if err != nil {
		return FileID{}, 0, fmt.Errorf("open file: %w", err)
	}
	defer syscall.CloseHandle(h)

	var info syscall.ByHandleFileInformation
	err = syscall.GetFileInformationByHandle(h, &info)
	if err != nil {
		return FileID{}, 0, fmt.Errorf("get file info: %w", err)
	}

	// On Windows, combine volume serial number with file index to create unique identifier
	// Device = VolumeSerialNumber, Inode = (FileIndexHigh << 32) | FileIndexLow
	fileID := FileID{
		Device: uint64(info.VolumeSerialNumber),
		Inode:  (uint64(info.FileIndexHigh) << 32) | uint64(info.FileIndexLow),
	}

	return fileID, uint64(info.NumberOfLinks), nil
}

func isSymlink(fi os.FileInfo) bool {
	// Use instructions described at
	// https://devblogs.microsoft.com/oldnewthing/20100212-00/?p=14963
	// to recognize whether it's a symlink.
	if fi.Sys().(*syscall.Win32FileAttributeData).FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false
	}

	v := reflect.Indirect(reflect.ValueOf(fi))
	reserved0 := v.FieldByName("Reserved0").Uint()

	return reserved0 == syscall.IO_REPARSE_TAG_SYMLINK ||
		reserved0 == 0xA0000003
}
