// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	jsonresp "github.com/sylabs/json-resp"
)

func Test_Search(t *testing.T) {
	tests := []struct {
		description   string
		code          int
		body          interface{}
		reqCallback   func(*http.Request, *testing.T)
		searchArgs    map[string]string
		expectResults *SearchResults
		expectError   bool
	}{
		{
			description: "ValidRequest",
			searchArgs: map[string]string{
				"value": "test",
			},
			code:          http.StatusOK,
			body:          jsonresp.Response{Data: testSearch},
			expectResults: &testSearch,
			expectError:   false,
		},
		{
			description: "ValidRequestMultiArg",
			searchArgs: map[string]string{
				"value":  "test",
				"arch":   "x86_64",
				"signed": "true",
			},
			code:          http.StatusOK,
			body:          jsonresp.Response{Data: testSearch},
			expectResults: &testSearch,
			expectError:   false,
		},
		{
			description: "InternalServerError",
			searchArgs:  map[string]string{"value": "test"},
			code:        http.StatusInternalServerError,
			expectError: true,
		},
		{
			description: "BadRequest",
			searchArgs:  map[string]string{},
			code:        http.StatusBadRequest,
			expectError: true,
		},
		{
			description: "InvalidValue",
			searchArgs:  map[string]string{"value": "aa"},
			code:        http.StatusBadRequest,
			expectError: true,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {

			m := mockService{
				t:           t,
				code:        tt.code,
				body:        tt.body,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/search",
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			results, err := c.Search(context.Background(), tt.searchArgs)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if !reflect.DeepEqual(results, tt.expectResults) {
				t.Errorf("Got created collection %v - expected %v", results, tt.expectResults)
			}
		})
	}
}
