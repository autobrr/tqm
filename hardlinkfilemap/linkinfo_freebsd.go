package hardlinkfilemap

import (
	"errors"
	"os"
	"strconv"
	"syscall"
)

func LinkInfo(fi os.FileInfo, _ string) (string, uint64, error) {
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", 0, errors.New("failed to get file identifier")
	}

	return strconv.FormatUint(sys.Dev, 10) + "|" + strconv.FormatUint(sys.Ino, 10),
		uint64(sys.Nlink),
		nil
}
