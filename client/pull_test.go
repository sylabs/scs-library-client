// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/golang/glog"
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
		glog.Fatalf("Test HTTP server unable to output file: %v", err)
	}

}

func Test_DownloadImage(t *testing.T) {

	f, err := ioutil.TempFile("", "test")
	if err != nil {
		t.Fatalf("Error creating a temporary file for testing")
	}
	tempFile := f.Name()
	f.Close()
	os.Remove(tempFile)

	tests := []struct {
		name         string
		path         string
		tag          string
		outFile      string
		code         int
		testFile     string
		tokenFile    string
		checkContent bool
		expectError  bool
	}{
		{"Bad library ref", "entity/collection/im,age", "tag", tempFile, http.StatusBadRequest, "test_data/test_sha256", "test_data/test_token", false, true},
		{"Server error", "entity/collection/image", "tag", tempFile, http.StatusInternalServerError, "test_data/test_sha256", "test_data/test_token", false, true},
		{"Tags in path", "entity/collection/image:tag", "anothertag", tempFile, http.StatusOK, "test_data/test_sha256", "test_data/test_token", false, true},
		{"Good Download", "entity/collection/image", "tag", tempFile, http.StatusOK, "test_data/test_sha256", "test_data/test_token", true, false},
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

			out, err := os.OpenFile(tt.outFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0777)
			if err != nil {
				t.Errorf("Error opening file %s for writing: %v", tt.outFile, err)
			}

			err = c.DownloadImage(out, tt.path, tt.tag, nil)

			out.Close()

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}

			if tt.checkContent {
				fileContent, err := ioutil.ReadFile(tt.outFile)
				if err != nil {
					t.Errorf("Error reading test output file: %v", err)
				}
				testContent, err := ioutil.ReadFile(tt.testFile)
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
