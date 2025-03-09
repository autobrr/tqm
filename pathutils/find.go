package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/autobrr/tqm/logger"
)

/* Structs */

type Path struct {
	Path         string
	RealPath     string
	FileName     string
	Directory    string
	IsDir        bool
	Size         int64
	ModifiedTime time.Time
}

/* Types */

type callbackAllowed func(string) *string

/* Vars */

var (
	log = logger.GetLogger("pathutils")
)

/* Public */

func GetPathsInFolder(folder string, includeFiles bool, includeFolders bool, acceptFn callbackAllowed) ([]Path, uint64) {
	var paths []Path
	var size uint64 = 0
	var mutex sync.Mutex
	var wg sync.WaitGroup

	entries, err := os.ReadDir(folder)
	if err != nil {
		log.WithError(err).Errorf("Failed to list directory %s", folder)
		return paths, size
	}

	for _, entry := range entries {
		wg.Add(1)
		go func(entry os.DirEntry) {
			defer wg.Done()

			entryPath := filepath.Join(folder, entry.Name())
			info, err := entry.Info()
			if err != nil {
				log.WithError(err).Errorf("Failed to get file info for %s", entryPath)
				return
			}

			if processEntry(entryPath, info, includeFolders, includeFiles, acceptFn, &paths, &size, &mutex) {
				if info.IsDir() {
					subpaths, subsize := walkDirectory(entryPath, includeFiles, includeFolders, acceptFn)

					mutex.Lock()
					paths = append(paths, subpaths...)
					size += subsize
					mutex.Unlock()
				}
			}
		}(entry)
	}

	wg.Wait()
	return paths, size
}

// walkDirectory handles walking a single directory tree (non-parallel for deeper levels)
func walkDirectory(folder string, includeFiles bool, includeFolders bool, acceptFn callbackAllowed) ([]Path, uint64) {
	var paths []Path
	var size uint64 = 0

	err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk func: %w", err)
		}

		if path == folder {
			return nil
		}

		processEntry(path, info, includeFolders, includeFiles, acceptFn, &paths, &size, nil)
		return nil
	})

	if err != nil {
		log.WithError(err).Errorf("Failed to retrieve paths from %s", folder)
	}

	return paths, size
}

// processEntry handles a single file/directory entry
func processEntry(path string, info os.FileInfo, includeFolders bool, includeFiles bool,
	acceptFn callbackAllowed, paths *[]Path, size *uint64, mutex *sync.Mutex) bool {

	if !includeFiles && !info.IsDir() {
		log.Tracef("Skipping file: %s", path)
		return false
	}

	if !includeFolders && info.IsDir() {
		log.Tracef("Skipping folder: %s", path)
		return false
	}

	realPath := path
	finalPath := path
	if acceptFn != nil {
		if acceptedPath := acceptFn(path); acceptedPath == nil {
			log.Tracef("Skipping rejected path: %s", path)
			return false
		} else {
			finalPath = *acceptedPath
		}
	}

	foundPath := Path{
		Path:         finalPath,
		RealPath:     realPath,
		FileName:     info.Name(),
		Directory:    filepath.Dir(path),
		IsDir:        info.IsDir(),
		Size:         info.Size(),
		ModifiedTime: info.ModTime(),
	}

	if mutex != nil {
		mutex.Lock()
		*paths = append(*paths, foundPath)
		*size += uint64(info.Size())
		mutex.Unlock()
	} else {
		*paths = append(*paths, foundPath)
		*size += uint64(info.Size())
	}

	return true
}
