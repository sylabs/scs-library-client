// Copyright (c) 2019, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the LICENSE.md file
// distributed with the sources of this project regarding your rights to use or distribute this
// software.

package client

// DummyLogger is a sink for log messages in the absence of a capable
// logger.
type DummyLogger struct {
	Logger
}

// Info outputs at info level
func (DummyLogger) Info(...interface{}) {
	// do nothing
}

// Infof takes a format string and args
func (DummyLogger) Infof(string, ...interface{}) {
	// do nothing
}

// Debug outputs at debug level
func (DummyLogger) Debug(...interface{}) {
	// do nothing
}

// Debugf takes a format string and args
func (DummyLogger) Debugf(string, ...interface{}) {
	// do nothing
}

// Warning logs at warning level
func (DummyLogger) Warning(args ...interface{}) {
	// do nothing
}

// Warningf takes a format string and args
func (DummyLogger) Warningf(format string, args ...interface{}) {
	// do nothing
}
