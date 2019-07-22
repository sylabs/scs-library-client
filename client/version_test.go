// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"testing"

	jsonresp "github.com/sylabs/json-resp"
)

func Test_apiAtLeast(t *testing.T) {
	type legacyVersionInfo struct {
		Version string `json:"version"`
	}

	ctx := context.Background()

	tests := []struct {
		description     string
		body            interface{}
		code            int
		isV2APIUpload   bool
		isV2APIArchTags bool
	}{
		{"legacy", legacyVersionInfo{Version: "1.0.0-alpha.1"}, 200, false, false},
		{"malformed", legacyVersionInfo{}, 200, false, false},
		{"error", nil, 404, false, false},
		{"current", VersionInfo{Version: "1.0.0-alpha.1", APIVersion: "2.0.0-alpha.2"}, 200, true, true},
		{"slightly older", VersionInfo{Version: "1.0.0-alpha.1", APIVersion: "2.0.0-alpha.1"}, 200, true, false},
		{"newer than that", VersionInfo{Version: "1.0.0-alpha.1", APIVersion: "2.0.5-alpha.1"}, 200, true, true},
		{"distant future", VersionInfo{Version: "1.0.0-alpha.1", APIVersion: "3.0.0"}, 200, true, true},
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

			result := c.apiAtLeast(ctx, APIVersionV2Upload)
			if result && !tt.isV2APIUpload {
				t.Errorf("Unexpected true for API version not supporting V2 Upload.")
			}
			if !result && tt.isV2APIUpload {
				t.Errorf("Unexpected false for API version supporting V2 Upload.")
			}

			result = c.apiAtLeast(ctx, APIVersionV2ArchTags)
			if result && !tt.isV2APIArchTags {
				t.Errorf("Unexpected true for API version not supporting V2 ArchTags.")
			}
			if !result && tt.isV2APIArchTags {
				t.Errorf("Unexpected false for API version supporting V2 ArchTags.")
			}

		})
	}
}
