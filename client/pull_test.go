// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

type mockRawService struct {
	t           *testing.T
	code        int
	testFile    string
	reqCallback func(*http.Request, *testing.T)
	httpAddr    string
	httpPath    string
	httpServer  *httptest.Server
	baseURI     string
}

func (m *mockRawService) Run() {
	mux := http.NewServeMux()
	mux.HandleFunc(m.httpPath, m.ServeHTTP)
	m.httpServer = httptest.NewServer(mux)
	m.httpAddr = m.httpServer.Listener.Addr().String()
	m.baseURI = "http://" + m.httpAddr
}

func (m *mockRawService) Stop() {
	m.httpServer.Close()
}

func (m *mockRawService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.reqCallback != nil {
		m.reqCallback(r, m.t)
	}
	w.WriteHeader(m.code)
	inFile, err := os.Open(m.testFile)
	if err != nil {
		m.t.Errorf("error opening file %v:", err)
	}
	defer inFile.Close()

	_, err = io.Copy(w, bufio.NewReader(inFile))
	if err != nil {
		m.t.Errorf("Test HTTP server unable to output file: %v", err)
	}
}

func Test_DownloadImage(t *testing.T) {
	f, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatalf("Error creating a temporary file for testing")
	}
	tempFile := f.Name()
	f.Close()
	os.Remove(tempFile)

	tests := []struct {
		name         string
		arch         string
		path         string
		tag          string
		outFile      string
		code         int
		testFile     string
		tokenFile    string
		checkContent bool
		expectError  bool
	}{
		{"Bad library ref", "amd64", "entity/collection/im,age", "tag", tempFile, http.StatusBadRequest, "test_data/test_sha256", "test_data/test_token", false, true},
		{"Server error", "amd64", "entity/collection/image", "tag", tempFile, http.StatusInternalServerError, "test_data/test_sha256", "test_data/test_token", false, true},
		{"Tags in path", "amd64", "entity/collection/image:tag", "anothertag", tempFile, http.StatusOK, "test_data/test_sha256", "test_data/test_token", false, true},
		{"Good Download", "amd64", "entity/collection/image", "tag", tempFile, http.StatusOK, "test_data/test_sha256", "test_data/test_token", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mockRawService{
				t:        t,
				code:     tt.code,
				testFile: tt.testFile,
				httpPath: fmt.Sprintf("/v1/imagefile/%s:%s", tt.path, tt.tag),
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: tt.tokenFile, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			out, err := os.OpenFile(tt.outFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o777)
			if err != nil {
				t.Errorf("Error opening file %s for writing: %v", tt.outFile, err)
			}

			err = c.DownloadImage(context.Background(), out, tt.arch, tt.path, tt.tag, nil)

			out.Close()

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}

			if tt.checkContent {
				fileContent, err := os.ReadFile(tt.outFile)
				if err != nil {
					t.Errorf("Error reading test output file: %v", err)
				}
				testContent, err := os.ReadFile(tt.testFile)
				if err != nil {
					t.Errorf("Error reading test file: %v", err)
				}
				if !bytes.Equal(fileContent, testContent) {
					t.Errorf("File contains '%v' - expected '%v'", fileContent, testContent)
				}
			}
		})
	}
}

func TestIsSameDomain(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		redirectURL string
		same        bool
	}{
		{"Equal", "https://library.domain.com", "https://library.domain.com", true},
		{"SameSubdomain", "https://cloud.domain.com", "https://library.cloud.domain.com", true},
		{"EqualWithDifferentPorts", "https://library.domain.com:1234", "https://library.domain.com:5678", true},
		{"EqualOneWithPort", "https://library.domain.com", "https://library.domain.com:1234", true},
		{"EqualOtherWithPort", "https://library.domain.com:1234", "https://library.domain.com", true},
		{"NotEqual", "https://library.othersite.com", "https://library.domain.com", false},
		{"NotEqualReversed", "https://library.domain.com", "https://library.othersite.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u1, _ := url.Parse(tt.baseURL)
			u2, _ := url.Parse(tt.redirectURL)

			result := isSameDomain(u1, u2)
			if result != tt.same {
				t.Fatalf("Mismatch %v is not same/subdomain of %v", u2.String(), u1.String())
			}
		})
	}
}
