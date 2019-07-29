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
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	//LogDebug logger
	LogDebug *log.Logger
	//LogInfo logger
	LogInfo *log.Logger
	//LogError logger
	LogError *log.Logger
	logOut   *lumberjack.Logger
	// DebugLogLevel debug log level
	DebugLogLevel = "debug"
	// InfoLogLevel info log level
	InfoLogLevel = "info"
)

func init() {
	LogDebug = log.New(ioutil.Discard, "Debug: ", log.Ldate|log.Ltime|log.Lshortfile)
	LogInfo = log.New(ioutil.Discard, "Info : ", log.Ldate|log.Ltime|log.Lshortfile)
	LogError = log.New(ioutil.Discard, "Error: ", log.Ldate|log.Ltime|log.Lshortfile)
}

//OpenLogFile creates a file based logger
func OpenLogFile(filePath string, maxSizeMB int, maxFiles int, maxAgeDays int, debug bool) error {
	if logOut != nil {
		return fmt.Errorf("a log file is already open. %s", logOut.Filename)
	}

	pathCheck(filePath)
	logOut = &lumberjack.Logger{
		Filename:   filePath,
		MaxSize:    maxSizeMB,
		MaxBackups: maxFiles,
		MaxAge:     maxAgeDays,
	}

	if debug {
		LogDebug = log.New(logOut, "Debug: ", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		LogDebug = log.New(ioutil.Discard, "Debug: ", log.Ldate|log.Ltime|log.Lshortfile)
	}
	LogInfo = log.New(logOut, "Info : ", log.Ldate|log.Ltime|log.Lshortfile)
	LogError = log.New(logOut, "Error: ", log.Ldate|log.Ltime|log.Lshortfile)
	return nil
}

//OpenLog causes logging to happen to stdout
func OpenLog(debug bool) error {
	stdLog := log.New(os.Stdout, "", log.Ltime|log.Lshortfile)

	if debug {
		LogDebug = stdLog
	} else {
		LogDebug = log.New(ioutil.Discard, "", log.Ltime|log.Lshortfile)
	}
	LogInfo = stdLog
	LogError = stdLog
	return nil
}

// create dirs if needed
func pathCheck(filePath string) {
	dir := filepath.Dir(filePath)
	if len(dir) > 1 {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			fmt.Fprintln(os.Stderr, fmt.Sprintf("Unable to create %s (%s)", dir, err))
		}
	}
}

//CloseLogFile closes the current logFile
func CloseLogFile() {
	if logOut == nil {
		return
	}
	logOut.Close()
	logOut = nil
}

// HTTPLogger : wrapper for http logging
func HTTPLogger(inner http.Handler, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LogInfo.Printf(
			">>>>> %s\t%s\t%s",
			r.Method,
			r.RequestURI,
			name,
		)

		start := time.Now()
		inner.ServeHTTP(w, r)

		LogInfo.Printf(
			"<<<<< %s\t%s\t%s\t%s",
			r.Method,
			r.RequestURI,
			name,
			time.Since(start),
		)
	})
}

// Scrubber checks if the args list contains any sensitive information like username/password/secret
// If found, then returns masked string list, else returns the original input list unmodified.
func Scrubber(args []string) []string {
	badwords := []string{"username", "user", "password", "passwd", "secret"}
	for _, arg := range args {
		for _, bad := range badwords {
			if strings.Contains(arg, bad) {
				return []string{"**********"}
			}
		}
	}
	return args
}
