package paths

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charlievieth/fastwalk"

	"github.com/autobrr/tqm/pkg/logger"
)

type Path struct {
	Path         string
	RealPath     string
	FileName     string
	Directory    string
	IsDir        bool
	Size         int64
	ModifiedTime time.Time
}

type callbackAllowed func(string) *string

var (
	log = logger.GetLogger("paths")
)

// InFolder traverses the provided folder and returns a list of paths and their total size.
// Files and folders can optionally be included in the results, and a custom accept function can be provided to
// filter the results further.
func InFolder(folder string, includeFiles bool, includeFolders bool, acceptFn callbackAllowed) ([]Path, uint64) {
	var paths []Path
	var size uint64 = 0
	var mutex sync.Mutex

	conf := fastwalk.Config{
		Follow: false,
	}

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.WithError(err).Errorf("Error accessing path %q during walk", path)
			if os.IsPermission(err) {
				log.Warnf("Permission error on %q, continuing walk if possible...", path)
			}
			return nil
		}

		if path == folder {
			return nil
		}

		isDir := d.IsDir()

		if !includeFiles && !isDir {
			log.Tracef("Skipping file: %s", path)
			return nil
		}

		if !includeFolders && isDir {
			log.Tracef("Skipping folder: %s", path)
			return nil
		}

		realPath := path
		finalPath := path
		if acceptFn != nil {
			if acceptedPath := acceptFn(path); acceptedPath == nil {
				log.Tracef("Skipping rejected path: %s", path)
				return nil
			} else {
				finalPath = *acceptedPath
			}
		}

		info, err := d.Info()
		if err != nil {
			log.WithError(err).Errorf("Failed to get file info for %s", path)
			return nil
		}

		foundPath := Path{
			Path:         finalPath,
			RealPath:     realPath,
			FileName:     info.Name(),
			Directory:    filepath.Dir(path),
			IsDir:        isDir,
			Size:         info.Size(),
			ModifiedTime: info.ModTime(),
		}

		mutex.Lock()
		paths = append(paths, foundPath)
		size += uint64(info.Size())
		mutex.Unlock()

		return nil
	}

	err := fastwalk.Walk(&conf, folder, walkFn)
	if err != nil {
		log.WithError(err).Errorf("Failed to walk directory %s", folder)
	}

	return paths, size
}

// IsIgnored checks if a path is in the provided ignore list
func IsIgnored(path string, ignoreList []string) bool {
	return slices.ContainsFunc(ignoreList, func(s string) bool {
		return strings.HasPrefix(path, s)
	})
}

// IsDirEmpty checks if the provided path is an empty dir
func IsDirEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// Read exactly one entry. If EOF, the directory is empty.
	// If we get any entry, it's not empty. Poetry.
	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	return false, nil
}
