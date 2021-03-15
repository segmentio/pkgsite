// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package log

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"cloud.google.com/go/logging"
)

const (
	debugMsg = "debugMsg"
	infoMsg  = "infoMsg"
	errorMsg = "errorMsg"
)

// Do not run in parallel. It overrides currentLevel.
func TestSetLogLevel(t *testing.T) {
	oldLevel := getLevel()
	defer func() { currentLevel = oldLevel }()

	tests := []struct {
		name      string
		newLevel  string
		wantLevel logging.Severity
	}{
		{name: "default level", newLevel: "", wantLevel: logging.Default},
		{name: "invalid level", newLevel: "xyz", wantLevel: logging.Default},
		{name: "debug level", newLevel: "debug", wantLevel: logging.Debug},
		{name: "info level", newLevel: "info", wantLevel: logging.Info},
		{name: "warning level", newLevel: "warning", wantLevel: logging.Warning},
		{name: "error level", newLevel: "error", wantLevel: logging.Error},
		{name: "fatal level", newLevel: "fatal", wantLevel: logging.Critical},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			SetLevel(test.newLevel)
			gotLevel := getLevel()
			if test.wantLevel != gotLevel {
				t.Errorf("Error: want=%s, got=%s", test.wantLevel, gotLevel)
			}
		})
	}
}

// Do not run in parallel. It overrides logger with mockLogger.
func TestLogLevel(t *testing.T) {
	oldLogger := logger
	defer func() { logger = oldLogger }()
	logger = &mockLogger{}

	// logs below info(like debug) won't print
	SetLevel("info")

	tests := []struct {
		name     string
		logFunc  func(context.Context, interface{})
		logMsg   string
		expected bool
	}{
		{name: "debug", logFunc: Debug, logMsg: debugMsg, expected: false},
		{name: "info", logFunc: Info, logMsg: infoMsg, expected: true},
		{name: "warning", logFunc: Warning, logMsg: infoMsg, expected: true},
		{name: "error", logFunc: Error, logMsg: errorMsg, expected: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			test.logFunc(context.Background(), test.logMsg)
			logs := logger.(*mockLogger).logs
			got := strings.Contains(logs, test.logMsg)

			if got != test.expected {
				t.Errorf("expected : %v, got %v", test.expected, got)
			}
		})
	}
}

// Do not run in parallel. It overrides logger with mockLogger.
func TestDefaultLogLevel(t *testing.T) {
	oldLogger := logger
	defer func() { logger = oldLogger }()
	logger = &mockLogger{}

	SetLevel("") // default behaviour; print everything

	tests := []struct {
		name    string
		logFunc func(context.Context, interface{})
		logMsg  string
	}{
		{name: "debug", logFunc: Debug, logMsg: debugMsg},
		{name: "info", logFunc: Info, logMsg: infoMsg},
		{name: "error", logFunc: Error, logMsg: errorMsg},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			test.logFunc(context.Background(), test.logMsg)
			logs := logger.(*mockLogger).logs

			if !strings.Contains(logs, test.logMsg) {
				t.Errorf("%v not logged.", test.logMsg)
			}
		})
	}
}

type mockLogger struct {
	logs string
}

func (l *mockLogger) log(ctx context.Context, s logging.Severity, payload interface{}) {
	l.logs += fmt.Sprintf("%s: %+v", s, payload)
}
