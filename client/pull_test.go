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
	"strings"
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

func TestSameHost(t *testing.T) {
	tests := []struct {
		name  string
		host1 string
		host2 string
		same  bool
	}{
		{"Simple", "http://testhost", "http://testhost", true},
		{"SimpleHTTPS", "https://testhost", "https://testhost", true},
		{"SimpleWithPort", "http://testhost:1234", "http://testhost:1234", true},
		{"MismatchedScheme", "https://anotherhost", "http://anotherhost", false},
		{"DifferentHostNames", "https://testhost1", "https://testhost2", false},
		{"DifferentHostNamesAndScheme", "http://testhost1", "https://testhost2", false},
		{"WithCreds", "http://user:password@testhost1", "http://testhost1", true},
		{"FullyQualified", "http://testhost.testdomain", "http://testhost.testdomain", true},
		{"QualifiedVsNot", "http://testhost", "http://testhost.testdomain", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host1, _ := url.Parse(tt.host1)
			host2, _ := url.Parse(tt.host2)

			result := samehost(host1, host2)
			if got, want := result, tt.same; got != want {
				t.Fatalf("Unexpected results: host1 %v, host2 %v: got %v, want %v)", host1.String(), host2.String(), got, want)
			}
		})
	}
}

// parseAuthorizationHeader parses 'authorizationHeader' and succeeds if properly
// formed ("Authorization: Bearer <TOKEN>") and bearer tokens match.
func parseAuthorizationHeader(t *testing.T, authorizationHeader, authToken string) bool {
	t.Helper()

	if authorizationHeader == "" {
		return false
	}

	authHeaderElements := strings.SplitN(authorizationHeader, " ", 2)
	if len(authHeaderElements) != 2 {
		t.Fatalf("Malformed Authorization header: %v", authorizationHeader)
	}

	if !strings.EqualFold(authHeaderElements[0], "Bearer") {
		t.Fatalf("Malformed Authorization header: %v", authorizationHeader)
	}

	if (authHeaderElements[1] != "" && authToken == "") || authHeaderElements[1] != authToken {
		t.Fatalf("Unexpected token: %v", authorizationHeader)
	}

	return true
}

func parseRangeHeader(t *testing.T, value string, expectedStart, expectedEnd int64) {
	if got, want := strings.ToLower(value), fmt.Sprintf("bytes=%d-%d", expectedStart, expectedEnd); got != want {
		t.Fatalf("unexpected Range header: got %v, want %v", got, want)
	}
}

// testAuthToken doesn't need to be a valid token, just a non-empty string
const testAuthToken = "xxxxx"

func TestHTTPGetRangeRequest(t *testing.T) {
	tests := []struct {
		name                      string
		redirectToSameHost        bool
		authToken                 string
		rangeStart                int64
		rangeEnd                  int64
		expectError               bool
		expectAuthorizationHeader bool
	}{
		{"SameHostWithAuthToken", true, testAuthToken, 0, 64 * 1024, false, true},
		{"SameHostWithoutAuthToken", true, "", 0, 64 * 1024 * 1024, false, false},
		{"DifferentHostWithAuthToken", false, testAuthToken, 0, 64 * 1024, false, false},
		{"InvalidRangeStartWithoutAuthToken", true, "", -1, 64 * 1024, true, false},
		{"InvalidRangeEndWithoutAuthToken", true, "", 0, -1, true, false},
		{"ZeroLengthRangeWithoutAuthToken", true, "", 65536, 65536, true, false},
		{"NegativeRangeWithoutAuthToken", true, "", 65536, 65535, true, false},
		{"InvalidRangeStartWithAuthToken", true, "", -1, 64 * 1024, true, false},
		{"InvalidRangeEndWithAuthToken", true, testAuthToken, 0, -1, true, false},
		{"ZeroLengthRangeWithAuthToken", true, testAuthToken, 65536, 65536, true, false},
		{"NegativeRangeWithAuthToken", true, testAuthToken, 65536, 65535, true, false},
		{"DifferentHostInvalidRangeStart", false, "", -1, 64 * 1024, true, false},
		{"DifferentHostInvalidRangeEnd", false, "", 0, -1, true, false},
		{"DifferentHostZeroLengthRange", false, "", 65536, 65536, true, false},
		{"DifferentHostNegativeRange", false, "", 65536, 65535, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var authorizationHeaderPresent bool

			cloudLibrarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusPartialContent)

				parseRangeHeader(t, r.Header.Get("Range"), tt.rangeStart, tt.rangeEnd)

				authorizationHeaderPresent = parseAuthorizationHeader(t, r.Header.Get("Authorization"), tt.authToken)
			}))
			defer cloudLibrarySrv.Close()

			// fileSrv mocks an alternate server than test cloud library server
			fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				parseRangeHeader(t, r.Header.Get("Range"), tt.rangeStart, tt.rangeEnd)

				authorizationHeaderPresent = parseAuthorizationHeader(t, r.Header.Get("Authorization"), tt.authToken)

				w.WriteHeader(http.StatusPartialContent)
			}))
			defer fileSrv.Close()

			endpoint := cloudLibrarySrv.URL
			if !tt.redirectToSameHost {
				// Use alternate endpoint
				endpoint = fileSrv.URL
			}

			c, err := NewClient(&Config{BaseURL: cloudLibrarySrv.URL, AuthToken: tt.authToken})
			if err != nil {
				t.Fatalf("Error initializing client: %v", err)
			}

			res, err := c.httpGetRangeRequest(context.Background(), endpoint, tt.rangeStart, tt.rangeEnd)
			if err != nil {
				if !tt.expectError {
					t.Fatalf("Unexpected error: %v", err)
				}
				return
			}
			defer res.Body.Close()

			if err == nil && tt.expectError {
				t.Fatal("Unexpected success")
			}

			if got, want := authorizationHeaderPresent, tt.expectAuthorizationHeader; got != want {
				t.Fatalf("unexpected authorization header: got %v, want %v", got, want)
			}
		})
	}
}
