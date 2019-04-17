// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"testing"
)

// TestLogger is used to relay messages from the logging subsystem to
// the standard Go testing output method
type TestLogger struct {
	T *testing.T
}

// Info handles informational messages
func (TestLogger) Info(...interface{}) {
}

// Infof takes a format string and args
func (TestLogger) Infof(string, ...interface{}) {
}

// Error is for outputting an error message
func (log TestLogger) Error(args ...interface{}) {
	log.T.Log(args...)
}

// Errorf takes a format string and args
func (log TestLogger) Errorf(format string, args ...interface{}) {
	log.T.Logf(format, args...)
}

// Debug outputs debug level messages
func (log TestLogger) Debug(args ...interface{}) {
	log.T.Log(args...)
}

// Debugf takes a format string and args
func (log TestLogger) Debugf(format string, args ...interface{}) {
	log.T.Logf(format, args...)
}
