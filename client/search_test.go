// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"testing"

	jsonresp "github.com/sylabs/json-resp"
)

const (
	testSearchOutput = `Found 1 users for 'test'
	library://test-user

Found 1 collections for 'test'
	library://test-user/test-collection

Found 1 containers for 'test'
	library://test-user/test-collection/test-container
		Tags: latest test-tag

`

	testSearchOutputEmpty = `No users found for 'test'

No collections found for 'test'

No containers found for 'test'

`
)

func Test_Search(t *testing.T) {
	tests := []struct {
		description   string
		code          int
		body          interface{}
		reqCallback   func(*http.Request, *testing.T)
		value         string
		expectResults *SearchResults
		expectError   bool
	}{
		{
			description:   "ValidRequest",
			value:         "test",
			code:          http.StatusOK,
			body:          jsonresp.Response{Data: testSearch},
			expectResults: &testSearch,
			expectError:   false,
		},
		{
			description: "InternalServerError",
			value:       "test",
			code:        http.StatusInternalServerError,
			expectError: true,
		},
		{
			description: "BadRequest",
			value:       "test",
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

			results, err := c.Search(tt.value)

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

func Test_SearchLibrary(t *testing.T) {
	m := mockService{
		t:        t,
		code:     http.StatusOK,
		body:     jsonresp.Response{Data: testSearch},
		httpPath: "/v1/search",
	}

	m.Run()
	defer m.Stop()

	c, err := NewClient(&Config{BaseURL: m.baseURI})
	if err != nil {
		t.Errorf("Error initializing client: %v", err)
	}

	err = c.searchLibrary("a")
	if err == nil {
		t.Errorf("Search of 1 character shouldn't be submitted")
	}
	err = c.searchLibrary("ab")
	if err == nil {
		t.Errorf("Search of 2 characters shouldn't be submitted")
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = c.searchLibrary("test")
	if err != nil {
		t.Errorf("Search failed: %v", err)
	}

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outC <- buf.String()
	}()

	w.Close()
	os.Stdout = old
	out := <-outC

	if err != nil {
		t.Errorf("Search of test should succeed")
	}
	log.SetOutput(os.Stderr)

	if out != testSearchOutput {
		t.Errorf("Output of search not as expected")
		t.Errorf("=== EXPECTED ===")
		t.Errorf(testSearchOutput)
		t.Errorf("=== ACTUAL ===")
		t.Errorf(out)
	}
}

func Test_SearchLibraryEmpty(t *testing.T) {
	m := mockService{
		t:        t,
		code:     http.StatusOK,
		body:     jsonresp.Response{Data: SearchResults{}},
		httpPath: "/v1/search",
	}

	m.Run()
	defer m.Stop()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	c, err := NewClient(&Config{BaseURL: m.baseURI})
	if err != nil {
		t.Errorf("Error initializing client: %v", err)
	}

	err = c.searchLibrary("test")

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outC <- buf.String()
	}()

	w.Close()
	os.Stdout = old
	out := <-outC

	if err != nil {
		t.Errorf("Search of test should succeed")
	}
	log.SetOutput(os.Stderr)

	if out != testSearchOutputEmpty {
		t.Errorf("Output of search not as expected")
		t.Errorf("=== EXPECTED ===")
		t.Errorf(testSearchOutputEmpty)
		t.Errorf("=== ACTUAL ===")
		t.Errorf(out)
	}
}
