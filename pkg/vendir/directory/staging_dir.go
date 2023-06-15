// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: Apache-2.0

package directory

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar"
	ctlfetch "github.com/vmware-tanzu/carvel-vendir/pkg/vendir/fetch"
)

type StagingDir struct {
	rootDir     string
	stagingDir  string
	incomingDir string
}

func NewStagingDir() StagingDir {
	rootDir := ".vendir-tmp"
	return StagingDir{
		rootDir:     rootDir,
		stagingDir:  filepath.Join(rootDir, "staging"),
		incomingDir: filepath.Join(rootDir, "incoming"),
	}
}

func (d StagingDir) Prepare() error {
	err := d.cleanUpAll()
	if err != nil {
		return err
	}

	err = os.MkdirAll(d.stagingDir, 0700)
	if err != nil {
		return fmt.Errorf("Creating staging dir '%s': %s", d.stagingDir, err)
	}

	err = os.MkdirAll(d.incomingDir, 0700)
	if err != nil {
		return fmt.Errorf("Creating incoming dir '%s': %s", d.incomingDir, err)
	}

	return nil
}

func (d StagingDir) NewChild(path string) (string, error) {
	childPath := filepath.Join(d.stagingDir, path)
	childPathParent := filepath.Dir(childPath)

	err := os.MkdirAll(childPathParent, 0700)
	if err != nil {
		return "", fmt.Errorf("Creating directory '%s': %s", childPathParent, err)
	}

	return childPath, nil
}

func (d StagingDir) CopyExistingFiles(rootDir string, stagingPath string, ignorePaths []string) error {
	if len(ignorePaths) == 0 {
		return nil
	}

	// Create reference point from staging path to root
	rootPath := strings.Replace(stagingPath, d.stagingDir, rootDir, 1)

	if _, err := os.Stat(rootPath); os.IsNotExist(err) {
		return nil // Path does not exist so there is nothing to copy
	}

	var ip []string
	for _, ignorePath := range ignorePaths {
		ip = append(ip, filepath.Join(rootPath, ignorePath)) // Prefix ignore glob with destination path
	}

	// Consider WalkDir in the future for efficiency (Go 1.16)
	// Walk root path above to determine files that can be ignored
	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Verify that the path should be ignored
		ignored, err := isPathIgnored(path, ip)
		if err != nil {
			return err
		}

		if !ignored {
			return nil
		}

		stagingPath := strings.Replace(path, rootPath, stagingPath, 1) // Preserve structure from destination to staging

		// Ensure that the directories exist in the staging directory
		stagingDir := filepath.Dir(stagingPath)
		err = os.MkdirAll(stagingDir, 0700)
		if err != nil {
			return fmt.Errorf("Unable to create staging directory '%s': %s", stagingDir, err)
		}

		// Move the file to the staging directory
		err = copyFile(path, stagingPath)
		if err != nil {
			return fmt.Errorf("Moving source file '%s' to staging location '%s': %s", path, stagingPath, err)
		}
		return nil
	})
	if err == os.ErrNotExist {
		return nil
	}
	return err
}

func isPathIgnored(path string, ignorePaths []string) (bool, error) {

	for _, ip := range ignorePaths {
		ok, err := doublestar.PathMatch(ip, path)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (d StagingDir) Replace(path string) error {
	err := os.RemoveAll(path)
	if err != nil {
		return fmt.Errorf("Deleting dir %s: %s", path, err)
	}

	// Clean to avoid getting 'out/in/' from 'out/in/' instead of just 'out'
	parentPath := filepath.Dir(filepath.Clean(path))

	err = os.MkdirAll(parentPath, 0700)
	if err != nil {
		return fmt.Errorf("Creating final location parent dir %s: %s", parentPath, err)
	}

	err = os.Rename(d.stagingDir, path)
	if err != nil {
		return fmt.Errorf("Moving staging directory '%s' to final location '%s': %s", d.stagingDir, path, err)
	}

	return nil
}

func (d StagingDir) TempArea() StagingTempArea {
	return StagingTempArea{d.incomingDir}
}

func (d StagingDir) CleanUp() error {
	return d.cleanUpAll()
}

func (d StagingDir) cleanUpAll() error {
	err := os.RemoveAll(d.rootDir)
	if err != nil {
		return fmt.Errorf("Deleting tmp dir '%s': %s", d.rootDir, err)
	}
	return nil
}

type StagingTempArea struct {
	path string
}

var _ ctlfetch.TempArea = StagingTempArea{}

func (d StagingTempArea) NewTempDir(name string) (string, error) {
	tmpDir := filepath.Join(d.path, name)

	absTmpDir, err := filepath.Abs(tmpDir)
	if err != nil {
		return "", fmt.Errorf("Abs path '%s': %s", tmpDir, err)
	}

	err = os.Mkdir(absTmpDir, 0700)
	if err != nil {
		return "", fmt.Errorf("Creating incoming dir '%s' for %s: %s", absTmpDir, name, err)
	}

	return absTmpDir, nil
}

func (d StagingTempArea) NewTempFile(pattern string) (*os.File, error) {
	return ioutil.TempFile(d.path, pattern)
}

func copyFile(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("Unable to read file info: %s", src)
	}

	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("Unable to open file: %s", src)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("Unable to create destination file: %s", dst)
	}

	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("Copying into dst file: %s", err)
	}

	return nil
}
