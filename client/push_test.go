// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/globalsign/mgo/bson"
)

// Timeout for the main upload unit test
const pushTimeout = time.Duration(1800 * time.Second)

//func postFile(baseURL string, filePath string, imageID string) error {
func Test_postFile(t *testing.T) {

	tests := []struct {
		description string
		imageRef    string
		testFile    string
		code        int
		reqCallback func(*http.Request, *testing.T)
		expectError bool
	}{
		{
			description: "Container not found response",
			code:        404,
			reqCallback: nil,
			imageRef:    bson.NewObjectId().Hex(),
			testFile:    "test_data/test_sha256",
			expectError: true,
		},
		{
			description: "Unauthorized response",
			code:        401,
			reqCallback: nil,
			imageRef:    bson.NewObjectId().Hex(),
			testFile:    "test_data/test_sha256",
			expectError: true,
		},
		{
			description: "Valid Response",
			code:        200,
			reqCallback: nil,
			imageRef:    bson.NewObjectId().Hex(),
			testFile:    "test_data/test_sha256",
			expectError: false,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {

			m := mockService{
				t:        t,
				code:     tt.code,
				httpPath: "/v1/imagefile/" + tt.imageRef,
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			f, err := os.Open(tt.testFile)
			if err != nil {
				t.Errorf("Error opening file %s for reading: %v", tt.testFile, err)
			}
			defer f.Close()

			fi, err := f.Stat()
			if err != nil {
				t.Errorf("Error stats for file %s: %v", tt.testFile, err)
			}
			fileSize := fi.Size()

			_, err = f.Seek(0, io.SeekStart)
			if err != nil {
				t.Errorf("Error seeking in stream: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
			defer cancel()

			err = c.postFile(ctx, f, fileSize, tt.imageRef, nil)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
		})
	}
}
