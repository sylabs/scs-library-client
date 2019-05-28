// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"net/http"
	"os"
	"testing"

	jsonresp "github.com/sylabs/json-resp"
)

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
			imageRef:    "5cb9c34d7d960d82f5f5bc55",
			testFile:    "test_data/test_sha256",
			expectError: true,
		},
		{
			description: "Unauthorized response",
			code:        401,
			reqCallback: nil,
			imageRef:    "5cb9c34d7d960d82f5f5bc56",
			testFile:    "test_data/test_sha256",
			expectError: true,
		},
		{
			description: "Valid Response",
			code:        200,
			reqCallback: nil,
			imageRef:    "5cb9c34d7d960d82f5f5bc57",
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

			err = c.postFile(context.Background(), f, fileSize, tt.imageRef, nil)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
		})
	}
}

func Test_isV2API(t *testing.T) {
	type legacyVersionInfo struct {
		Version string `json:"version"`
	}

	ctx := context.Background()

	tests := []struct {
		description      string
		body             interface{}
		code             int
		isV2APIOrGreater bool
	}{
		{"legacy", legacyVersionInfo{Version: "1.0.0-alpha.1"}, 200, false},
		{"malformed", legacyVersionInfo{}, 200, false},
		{"error", nil, 404, false},
		{"current", VersionInfo{Version: "1.0.0-alpha.1", APIVersion: "2.0.0-alpha.1"}, 200, true},
		{"slightly newer", VersionInfo{Version: "1.0.0-alpha.1", APIVersion: "2.0.0-alpha.2"}, 200, true},
		{"newer than that", VersionInfo{Version: "1.0.0-alpha.1", APIVersion: "2.0.5-alpha.1"}, 200, true},
		{"distant future", VersionInfo{Version: "1.0.0-alpha.1", APIVersion: "3.0.0"}, 200, true},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:        t,
				code:     200,
				body:     jsonresp.Response{Data: tt.body},
				httpPath: "/version",
			}
			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			result := c.isV2API(ctx)
			if result && !tt.isV2APIOrGreater {
				t.Errorf("Unexpected V2 API. Expected V1 API")
			}
			if !result && tt.isV2APIOrGreater {
				t.Errorf("Unexpected V1 API. Expected V2 API or greater")
			}
		})
	}
}
