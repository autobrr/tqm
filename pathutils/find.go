package paths

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/autobrr/tqm/logger"
	"github.com/charlievieth/fastwalk"
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
	log = logger.GetLogger("pathutils")
)

func GetPathsInFolder(folder string, includeFiles bool, includeFolders bool, acceptFn callbackAllowed) ([]Path, uint64) {
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
