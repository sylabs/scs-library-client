// Copyright (c) 2018-2019, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func Test_apiUpdate(t *testing.T) {
	ctx := context.Background()

	type endpointResponse struct {
		Value string `json:"avalue"`
	}

	tests := []struct {
		description string
		code        int
		url         string
		body        interface{}
		expectError bool
	}{
		{"simple", 200, "v2/imagefile/5cb9c34d7d960d82f5f5bc54/_complete", nil, false},
		{"notfound", 404, "v2/nonexistent", nil, true},
		{"with_response", 200, "v2/withresponse", endpointResponse{Value: "hello"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:        t,
				code:     tt.code,
				httpPath: "/" + tt.url,
				body:     tt.body,
			}
			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			// use payload matching /v2/imagefile/{ref}/_complete endpoint. This
			// is an arbitrary test of apiUpdate not specific to this endpoint.
			res, err := c.apiUpdate(ctx, tt.url, UploadImageCompleteRequest{})
			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if tt.body != nil {
				var r endpointResponse
				err = json.Unmarshal(res, &r)
				if err != nil {
					t.Errorf("error decoding expected response: %v", err)
				}
				if !reflect.DeepEqual(r, tt.body) {
					t.Errorf("unexpected response")
				}
			}
		})
	}
}
