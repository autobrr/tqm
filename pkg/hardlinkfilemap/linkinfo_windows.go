package hardlinkfilemap

import (
	"os"
	"reflect"
	"strconv"
	"syscall"
)

type fileAttrs struct {
	nlink uint32
	vol   uint32
	idxhi uint32
	idxlo uint32
}

func isSymlink(fi os.FileInfo) bool {
	// Use instructions described at
	// https://blogs.msdn.microsoft.com/oldnewthing/20100212-00/?p=14963/
	// to recognize whether it's a symlink.
	if fi.Sys().(*syscall.Win32FileAttributeData).FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false
	}

	v := reflect.Indirect(reflect.ValueOf(fi))
	reserved0 := v.FieldByName("Reserved0").Uint()

	return reserved0 == syscall.IO_REPARSE_TAG_SYMLINK ||
		reserved0 == 0xA0000003
}

func getFileAttrs(fi os.FileInfo, p string) (fileAttrs, error) {
	pathp, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return fileAttrs{}, err
	}
	attrs := uint32(syscall.FILE_FLAG_BACKUP_SEMANTICS)
	if isSymlink(fi) {
		// Use FILE_FLAG_OPEN_REPARSE_POINT, otherwise CreateFile will follow symlink.
		// See https://docs.microsoft.com/en-us/windows/desktop/FileIO/symbolic-link-effects-on-file-systems-functions#createfile-and-createfiletransacted
		attrs |= syscall.FILE_FLAG_OPEN_REPARSE_POINT
	}
	h, err := syscall.CreateFile(pathp, 0, 0, nil, syscall.OPEN_EXISTING, attrs, 0)
	if err != nil {
		return fileAttrs{}, err
	}
	defer syscall.CloseHandle(h)
	var i syscall.ByHandleFileInformation
	err = syscall.GetFileInformationByHandle(h, &i)
	if err != nil {
		return fileAttrs{}, err
	}

	return fileAttrs{
		nlink: i.NumberOfLinks,
		vol:   i.VolumeSerialNumber,
		idxhi: i.FileIndexHigh,
		idxlo: i.FileIndexLow,
	}, nil
}

func LinkInfo(fi os.FileInfo, path string) (string, uint64, error) {
	attrs, err := getFileAttrs(fi, path)
	if err != nil {
		return "", 0, err
	}

	return strconv.FormatUint(uint64(attrs.vol), 10) + "|" + strconv.FormatUint(uint64(attrs.idxhi), 10) + "|" + strconv.FormatUint(uint64(attrs.idxlo), 10),
		uint64(attrs.nlink),
		nil
}
