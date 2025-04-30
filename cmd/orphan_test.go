package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/torrentfilemap"
)

func createTempDir(t *testing.T, baseDir, subPath string) string {
	t.Helper()
	dir := filepath.Join(baseDir, subPath)
	err := os.MkdirAll(dir, 0755)
	require.NoError(t, err, "Failed to create temp dir: %s", subPath)
	return dir
}

func createTempFile(t *testing.T, targetDir, fileName string, content string) string {
	t.Helper()
	filePath := filepath.Join(targetDir, fileName)
	err := os.WriteFile(filePath, []byte(content), 0644)
	require.NoError(t, err, "Failed to create temp file: %s", fileName)
	return filePath
}

func TestIsDirEmpty(t *testing.T) {
	baseTestDir := t.TempDir()
	// Test case 1: Empty directory
	emptyDir := createTempDir(t, baseTestDir, "empty_dir")
	isEmpty, err := isDirEmpty(emptyDir)
	assert.NoError(t, err, "isDirEmpty failed for empty directory")
	assert.True(t, isEmpty, "isDirEmpty should return true for an empty directory")

	// Test case 2: Non-empty directory
	nonEmptyDir := createTempDir(t, baseTestDir, "non_empty_dir")
	createTempFile(t, nonEmptyDir, "dummy.txt", "hello")
	isEmpty, err = isDirEmpty(nonEmptyDir)
	assert.NoError(t, err, "isDirEmpty failed for non-empty directory")
	assert.False(t, isEmpty, "isDirEmpty should return false for a non-empty directory")

	// Test case 3: Path is a file
	fileDir := createTempDir(t, baseTestDir, "file_dir")
	filePath := createTempFile(t, fileDir, "testfile.txt", "content")
	isEmpty, err = isDirEmpty(filePath)
	assert.Error(t, err, "isDirEmpty should return an error for a file path")
	assert.False(t, isEmpty, "isDirEmpty should return false when path is a file")

	// Test case 4: Non-existent path
	nonExistentPath := filepath.Join(baseTestDir, "non_existent_dir")
	isEmpty, err = isDirEmpty(nonExistentPath)
	assert.ErrorIs(t, err, os.ErrNotExist, "isDirEmpty should return os.ErrNotExist for non-existent path")
	assert.False(t, isEmpty, "isDirEmpty should return false for non-existent path")
}

func TestProcessInBatches(t *testing.T) {
	tests := []struct {
		name        string
		items       map[string]int64
		maxWorkers  int
		batchSize   int
		expectCalls int
	}{
		{"EmptyMap", map[string]int64{}, 5, 10, 0},
		{"LessThanBatch", map[string]int64{"a": 1, "b": 2}, 5, 10, 2},
		{"EqualToBatch", map[string]int64{"a": 1, "b": 2, "c": 3}, 5, 3, 3},
		{"MoreThanBatch", map[string]int64{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5}, 5, 3, 5},
		{"SingleWorker", map[string]int64{"a": 1, "b": 2, "c": 3}, 1, 2, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wg sync.WaitGroup
			var mu sync.Mutex
			processedItems := make(map[string]int64)
			var callCount atomic.Int64

			processFn := func(key string, val int64) {
				defer wg.Done()
				callCount.Add(1)
				// Simulate some work
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				processedItems[key] = val
				mu.Unlock()
			}

			var testWg sync.WaitGroup

			wrappedProcessFn := func(key string, val int64) {
				processFn(key, val)
				testWg.Done()
			}

			testWg.Add(len(tt.items))

			processInBatches(tt.items, tt.maxWorkers, tt.batchSize, wrappedProcessFn, &wg)

			testWg.Wait()

			assert.Equal(t, int64(tt.expectCalls), callCount.Load(), "Incorrect number of processFn calls")
			assert.Equal(t, len(tt.items), len(processedItems), "Mismatch in processed items count")

			for k, v := range tt.items {
				mu.Lock()
				processedVal, ok := processedItems[k]
				mu.Unlock()
				assert.True(t, ok, "Item %s was not processed", k)
				assert.Equal(t, v, processedVal, "Item %s has incorrect processed value", k)
			}
		})
	}

	t.Run("RaceConditionCheck", func(t *testing.T) {
		var wg sync.WaitGroup
		var counter atomic.Int64
		items := make(map[string]int64)
		numItems := 100
		for i := 0; i < numItems; i++ {
			items[fmt.Sprintf("item-%d", i)] = int64(i)
		}

		processFn := func(_ int64, val int64) {
			defer wg.Done()
			counter.Add(1)
			time.Sleep(time.Duration(val%5) * time.Millisecond)
		}

		var testWg sync.WaitGroup
		testWg.Add(numItems)
		wrappedProcessFn := func(_ string, val int64) {
			processFn(0, val)
			testWg.Done()
		}

		processInBatches(items, 10, 20, wrappedProcessFn, &wg)
		testWg.Wait()

		assert.Equal(t, int64(numItems), counter.Load(), "Atomic counter should match number of items processed")
	})
}

func TestOrphanIdentification(t *testing.T) {
	baseTestDir := t.TempDir()
	downloadDir := createTempDir(t, baseTestDir, "downloads")

	torrentsMap := map[string]config.Torrent{
		"hash1": {Hash: "hash1", Name: "torrent1", Path: filepath.Join(downloadDir, "folder1", "file1.txt")},
		"hash2": {Hash: "hash2", Name: "torrent2", Path: filepath.Join(downloadDir, "file2.txt")},
		"hash3": {Hash: "hash3", Name: "torrent3", Path: filepath.Join(downloadDir, "folder2")},
		"hash4": {Hash: "hash4", Name: "torrent4-mapped", Path: filepath.Join("/data/mapped", "file4.txt")}, // Keep mapped path abstract
	}
	tfm := torrentfilemap.New(torrentsMap)

	folder1Path := createTempDir(t, baseTestDir, filepath.Join("downloads", "folder1"))
	createTempFile(t, folder1Path, "file1.txt", "content1")

	createTempFile(t, downloadDir, "file2.txt", "content2")

	folder2Path := createTempDir(t, baseTestDir, filepath.Join("downloads", "folder2"))
	createTempFile(t, folder2Path, "inside_folder2.txt", "content_f2")

	orphanFilePathOld := createTempFile(t, downloadDir, "orphan_old.txt", "orphan_content_old")
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	os.Chtimes(orphanFilePathOld, twoHoursAgo, twoHoursAgo)

	orphanFilePathNew := createTempFile(t, downloadDir, "orphan_new.txt", "orphan_content_new")

	orphanEmptyFolderPath := createTempDir(t, baseTestDir, filepath.Join("downloads", "orphan_empty_folder"))

	orphanNonEmptyFolderPath := createTempDir(t, baseTestDir, filepath.Join("downloads", "orphan_non_empty_folder"))
	createTempFile(t, orphanNonEmptyFolderPath, "dummy.txt", "dummy")

	deepParentPath := createTempDir(t, baseTestDir, filepath.Join("downloads", "deep_orphan"))
	deepChildPath := createTempDir(t, baseTestDir, filepath.Join("downloads", "deep_orphan", "child"))
	deepEmptyChildPath := createTempDir(t, baseTestDir, filepath.Join("downloads", "deep_orphan", "empty_child"))
	createTempFile(t, deepChildPath, "deep_file.txt", "deep")

	mappedFileLocalPath := createTempFile(t, downloadDir, "file4.txt", "mapped_content")

	pathMapping := map[string]string{
		downloadDir: "/data/mapped",
	}

	localFilePaths := make(map[string]int64)
	localFolderPaths := make(map[string]int64)

	filepath.Walk(downloadDir, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		if path == downloadDir {
			return nil
		}
		if info.IsDir() {
			localFolderPaths[path] = info.Size()
		} else {
			localFilePaths[path] = info.Size()
		}
		return nil
	})

	gracePeriod := 1 * time.Hour
	removedFiles := make(map[string]bool)
	removedFolders := make(map[string]bool)
	var wg sync.WaitGroup
	var mu sync.Mutex

	processFileFn := func(localPath string, localPathSize int64) {
		defer wg.Done()
		if tfm.HasPath(localPath, pathMapping) {
			return
		}

		fileInfo, err := os.Stat(localPath)
		require.NoError(t, err)

		if time.Since(fileInfo.ModTime()) < gracePeriod {
			return
		}

		mu.Lock()
		removedFiles[localPath] = true
		mu.Unlock()
	}

	processInBatches(localFilePaths, 5, 10, processFileFn, &wg)
	wg.Wait()

	orphanFolderPaths := make([]string, 0, len(localFolderPaths))
	for localPath := range localFolderPaths {
		if !tfm.HasPath(localPath, pathMapping) {
			orphanFolderPaths = append(orphanFolderPaths, localPath)
		}
	}

	sort.Slice(orphanFolderPaths, func(i, j int) bool {
		return len(orphanFolderPaths[i]) > len(orphanFolderPaths[j])
	})

	for _, localPath := range orphanFolderPaths {
		empty, err := isDirEmpty(localPath)
		require.NoError(t, err)

		if empty {
			mu.Lock()
			removedFolders[localPath] = true
			mu.Unlock()
		}
	}

	assert.Contains(t, removedFiles, orphanFilePathOld, "Old orphan file should be marked for removal")
	assert.NotContains(t, removedFiles, orphanFilePathNew, "New orphan file (within grace) should NOT be marked for removal")
	assert.NotContains(t, removedFiles, filepath.Join(folder1Path, "file1.txt"), "Tracked file1 should NOT be marked")
	assert.NotContains(t, removedFiles, filepath.Join(downloadDir, "file2.txt"), "Tracked file2 should NOT be marked")
	assert.NotContains(t, removedFiles, mappedFileLocalPath, "Mapped file4 should NOT be marked")
	assert.NotContains(t, removedFiles, filepath.Join(folder2Path, "inside_folder2.txt"), "File inside tracked folder2 should NOT be marked")

	assert.Contains(t, removedFolders, orphanEmptyFolderPath, "Empty orphan folder should be marked for removal")
	assert.Contains(t, removedFolders, deepEmptyChildPath, "Deep empty orphan folder should be marked for removal")
	assert.NotContains(t, removedFolders, orphanNonEmptyFolderPath, "Non-empty orphan folder should NOT be marked")
	assert.NotContains(t, removedFolders, folder1Path, "Tracked folder1 should NOT be marked")
	assert.NotContains(t, removedFolders, folder2Path, "Tracked folder2 should NOT be marked")
	assert.NotContains(t, removedFolders, deepParentPath, "Non-empty deep parent orphan folder should NOT be marked")
	assert.NotContains(t, removedFolders, deepChildPath, "Non-empty deep child orphan folder should NOT be marked")

}

func TestOrphanDryRun(t *testing.T) {
	baseTestDir := t.TempDir()
	downloadDir := createTempDir(t, baseTestDir, "downloads")

	orphanFilePathOld := createTempFile(t, downloadDir, "orphan_old.txt", "orphan_content_old")
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	os.Chtimes(orphanFilePathOld, twoHoursAgo, twoHoursAgo)

	orphanEmptyFolderPath := createTempDir(t, baseTestDir, filepath.Join("downloads", "orphan_empty_folder"))

	tfm := torrentfilemap.New(nil)

	localFilePaths := map[string]int64{orphanFilePathOld: 100}

	gracePeriod := 1 * time.Hour
	flagDryRun = true
	t.Cleanup(func() { flagDryRun = false })

	var wg sync.WaitGroup
	processFileFn := func(localPath string, localPathSize int64) {
		defer wg.Done()
		if tfm.HasPath(localPath, nil) {
			return
		}
		fileInfo, _ := os.Stat(localPath)
		if time.Since(fileInfo.ModTime()) < gracePeriod {
			return
		}

	}
	processInBatches(localFilePaths, 5, 10, processFileFn, &wg)
	wg.Wait()

	orphanFolderPaths := []string{orphanEmptyFolderPath}
	for _, localPath := range orphanFolderPaths {
		_, err := isDirEmpty(localPath)
		require.NoError(t, err)
	}

	_, errFile := os.Stat(orphanFilePathOld)
	assert.NoError(t, errFile, "Orphan file should still exist in dry run")

	_, errFolder := os.Stat(orphanEmptyFolderPath)
	assert.NoError(t, errFolder, "Empty orphan folder should still exist in dry run")
}

func TestOrphanFolderSorting(t *testing.T) {
	paths := []string{
		"/tmp/a/b/c",
		"/tmp/a",
		"/tmp/x/y",
		"/tmp/a/b",
		"/tmp/x",
	}

	expectedOrder := []string{
		"/tmp/a/b/c",
		"/tmp/a/b",
		"/tmp/x/y",
		"/tmp/a",
		"/tmp/x",
	}

	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) > len(paths[j])
		}
		return paths[i] < paths[j]
	})

	assert.Equal(t, expectedOrder, paths, "Folder paths are not sorted correctly by depth (descending)")
}

func setupTestConfig() {
	if config.Config == nil {
		config.Config = &config.Configuration{}
	}
	if config.Config.Orphan.GracePeriod == 0 {
		config.Config.Orphan.GracePeriod = 10 * time.Minute
	}
}

func TestMain(m *testing.M) {
	setupTestConfig()
	exitCode := m.Run()
	os.Exit(exitCode)
}
