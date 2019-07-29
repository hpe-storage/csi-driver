/*
(c) Copyright 2017 Hewlett Packard Enterprise Development LP

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"bufio"
	"encoding/gob"
	"errors"
	"fmt"
	log "github.com/hpe-storage/common-host-libs/logger"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
)

// FileReadFirstLine read first line from a file
// TODO: make it OS independent
func FileReadFirstLine(path string) (line string, er error) {
	log.Trace(">>>>> FileReadFirstLine")
	defer log.Trace("<<<<< FileReadFirstLine")

	file, err := os.Open(path)
	a := ""
	if err != nil {
		log.Error(err)
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		a = scanner.Text()
		break
	}
	if err = scanner.Err(); err != nil {
		log.Error(err)
		return "", err
	}
	log.Trace(a)
	return a, err
}

//FileExists does a stat on the path and returns true if it exists
//In addition, dir returns true if the path is a directory
func FileExists(path string) (exists bool, dir bool, err error) {
	log.Tracef(">>>>> FileExists for path %s", path)
	defer log.Trace("<<<<< FileExists")

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return true, false, err
	}
	return true, info.IsDir(), nil
}

// IsFileSymlink to check if the path exists and if it's a symlink
func IsFileSymlink(path string) (exists bool, symlink bool, err error) {
	log.Tracef(">>>>> IsFileSymlink for path %s", path)
	defer log.Trace("<<<<< IsFileSymlink")

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return true, false, err
	}
	return true, (info.Mode()&os.ModeSymlink == os.ModeSymlink), nil
}

// CreateDirIfNotExists to create a directory if it is not already available
func CreateDirIfNotExists(dirPath string, perm os.FileMode) error {
	// Check if the directory already exists
	exists, isDir, err := FileExists(dirPath)
	if err != nil {
		return err
	}
	// Dir already created, so do nothing and return nil
	if exists && isDir {
		log.Traceln("Directory already exists: ", dirPath)
		return nil
	}
	// Create the new directory ( Also creates necessary parent dirs)
	if err = os.MkdirAll(dirPath, perm); err != nil {
		log.Errorln("Unable to create new directory: ", dirPath)
		return err
	}
	return nil
}

// FileGetStringsWithPattern  : get the filecontents as array of string matching pattern pattern
func FileGetStringsWithPattern(path string, pattern string) (filelines []string, err error) {
	log.Trace(">>>>> FileGetStringsWithPattern called with path: ", path, " Pattern: ", pattern)
	defer log.Trace("<<<<< FileGetStringsWithPattern")

	file, err := os.Open(path)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []string

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		log.Error(err)
		return nil, err
	}

	var matchingLines []string
	if pattern == "" {
		return lines, nil
	}
	r := regexp.MustCompile(pattern)
	for _, l := range lines {
		if r.MatchString(l) {
			log.Tracef("matching line :%s", l)
			matchString := r.FindAllStringSubmatch(l, -1)
			if matchString == nil || matchString[0] == nil {
				matchingLines = append(matchingLines, l)
			} else {
				matchingLines = append(matchingLines, matchString[0][1])
			}
		}
	}

	return matchingLines, err
}

// FileGetStrings : get the file contents as array of string
func FileGetStrings(path string) (line []string, err error) {

	return FileGetStringsWithPattern(path, "")
}

// FileWriteString : write line to the path
func FileWriteString(path, line string) (err error) {
	log.Tracef(">>>>> FileWriteString called with path: %s and string: %s ", path, line)
	defer log.Trace("<<<<< FileWriteString")

	var file *os.File
	is, _, err := FileExists(path)
	if !is {
		log.Debug("File doesn't exist, Creating : " + path)
		file, err = os.Create(path)
		defer file.Close()
		if err != nil {
			log.Errorf("cannot create file %s err %s", path, err.Error())
			return err
		}
	}
	err = os.Chmod(path, 0644)
	if err != nil {
		return err
	}
	file, err = os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		log.Error(err)
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	n, err := writer.WriteString(line)
	writer.Flush()
	if err != nil {
		log.Error("Unable to write to file " + path + " Err:" + err.Error())
		err = errors.New("Unable to write to file " + path + " Err:" + err.Error())
		return err
	}
	if n <= 0 {
		log.Warnf("File write did not go through as bytes written is %d: ", n)
	}
	log.Tracef("%d bytes written", n)

	return err
}

// FileWriteStrings writes all lines to file specified by path. Newline is appended to each line
func FileWriteStrings(path string, lines []string) (err error) {
	log.Tracef(">>>>> FileWriteStrings called with path: %s", path)
	defer log.Trace("<<<<< FileWriteStrings")
	var file *os.File
	is, _, err := FileExists(path)
	if !is {
		log.Debug("File does not exist, Creating : " + path)
		file, err = os.Create(path)
		defer file.Close()
		if err != nil {
			log.Errorf("FileWriteStrings: error creating file %s, err %s", path, err.Error())
			return err
		}
	}
	err = os.Chmod(path, 0644)
	if err != nil {
		return err
	}
	file, err = os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		log.Error(err)
		return err
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	defer func() {
		err = w.Flush()
	}()

	for _, line := range lines {
		// trim if newline is already appended to string, as we add later
		w.WriteString(strings.Trim(line, "\n"))
		// always add a new line so that caller doesn't need to append everytime
		w.WriteString("\n")
	}
	return err
}

//FileDelete : delete the file
func FileDelete(path string) error {
	log.Trace("File delete called")
	is, _, _ := FileExists(path)
	if !is {
		return errors.New("File doesnt exist " + path)
	}
	err := os.RemoveAll(path)
	if err != nil {
		return errors.New("Unable to delete file " + path + " " + err.Error())
	}
	return nil
}

// FileSaveGob : save the Gob file
func FileSaveGob(path string, object interface{}) error {
	log.Trace(">>>>> FileSaveGob called with ", path)
	defer log.Trace("<<<<< FileSaveGob")

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := gob.NewEncoder(file)
	encoder.Encode(object)
	// make sure this is read-only to non-root users
	err = os.Chmod(path, 0640)
	if err != nil {
		return err
	}
	return nil
}

// FileloadGob : Load and Decode Gob file
func FileloadGob(path string, object interface{}) error {
	log.Trace(">>>>> FileLoadGob called with ", path)
	defer log.Trace("<<<<< FileLoadGob")

	file, err := os.Open(path)
	defer file.Close()
	if err == nil {
		decoder := gob.NewDecoder(file)
		err = decoder.Decode(object)
	}
	return err
}

// FileCheck : checks for error
func FileCheck(e error) {
	log.Trace(">>>>> FileCheck")
	defer log.Trace("<<<<< FileCheck")
	if e != nil {
		log.Error("err :", e.Error())
		_, file, line, _ := runtime.Caller(1)
		log.Error("Line", line, "File", file, "Error:", e)
	}
}

// CopyFile copies a file from src to dst. If src and dst files exist, and are
// the same, then return success. Otherise, attempt to create a hard link
// between the two files. If that fail, copy the file contents from src to dst.
func CopyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !sfi.Mode().IsRegular() {
		// cannot copy non-regular files (e.g., directories,
		// symlinks, devices, etc.)
		return fmt.Errorf("CopyFile: non-regular source file %s (%q)", sfi.Name(), sfi.Mode().String())
	}
	dfi, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else {
		if !(dfi.Mode().IsRegular()) {
			return fmt.Errorf("CopyFile: non-regular destination file %s (%q)", dfi.Name(), dfi.Mode().String())
		}
		if os.SameFile(sfi, dfi) {
			return nil
		}
	}
	if err = os.Link(src, dst); err == nil {
		return nil
	}
	err = copyFileContents(src, dst)
	return err
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	err = out.Sync()
	return nil
}
