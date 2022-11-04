// Copyright (c) 2018-2022, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	crypto_rand "crypto/rand"

	math_rand "math/rand"
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

func TestParseContentRange(t *testing.T) {
	const hdr = "bytes 0-1000/1000"

	size, err := parseContentRange(hdr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := size, int64(1000); got != want {
		t.Fatalf("unexpected content length: got %v, want %v", got, want)
	}
}

func TestParseContentLengthHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		headerValue    string
		expectedResult int64
		expectError    bool
	}{
		{"ValidValue", "1234", 1234, false},
		{"InvalidValue", "xxxx", 0, true},
		{"EmptyValue", "", -1, false},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			result, err := parseContentLengthHeader(tt.headerValue)
			if !tt.expectError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expectError && err == nil {
				t.Fatal("unexpected success")
			}

			if got, want := result, tt.expectedResult; err == nil && got != want {
				t.Fatalf("unexpected result: got %v, want %v", got, want)
			}
		})
	}
}

func generateSampleData(t *testing.T) []byte {
	t.Helper()

	const maxSampleDataSize = 1 * 1024 * 1024 // 1 MiB

	size := math_rand.Int63() % maxSampleDataSize

	sampleBytes := make([]byte, size)

	if _, err := crypto_rand.Read(sampleBytes); err != nil {
		t.Fatalf("error generating random bytes: %v", err)
	}

	return sampleBytes
}

func seedRandomNumberGenerator(t *testing.T) {
	t.Helper()

	var b [8]byte
	if _, err := crypto_rand.Read(b[:]); err != nil {
		t.Fatalf("error seeding random number generator: %v", err)
	}
	math_rand.Seed(int64(binary.LittleEndian.Uint64(b[:])))
}

// mockLibraryServer returns *httptest.Server that mocks Cloud Library server; in particular,
// it has handlers for /version, /v1/images, /v1/imagefile, and /v1/imagepart
func mockLibraryServer(t *testing.T, sampleBytes []byte, size int64, multistream bool) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/version") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)

			if _, err := w.Write([]byte("{\"data\": {\"apiVersion\": \"1.0.0\"}}")); err != nil {
				t.Fatalf("error writing /version response: %v", err)
			}
			return
		}

		if multistream && strings.HasPrefix(r.URL.Path, "/v1/images/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)

			if _, err := w.Write([]byte(fmt.Sprintf("{\"data\": {\"size\": %v}}", size))); err != nil {
				t.Fatalf("error writing /v1/images response: %v", err)
			}

			return
		}

		if multistream && strings.HasPrefix(r.URL.Path, "/v1/imagefile/") {
			redirectURL := &url.URL{
				Scheme: "http",
				Host:   r.Host,
				Path:   "/v1/imagepart/" + strings.TrimPrefix(r.URL.Path, "/v1/imagefile/"),
			}
			w.Header().Set("Location", redirectURL.String())
			w.WriteHeader(http.StatusSeeOther)
			return
		}

		// Handle /v1/imagefile (single stream) or /v1/imagepart (multistream)

		// Handle Range request for multipart downloads
		var start, end int64
		if val := r.Header.Get("Range"); val != "" {
			start, end = parseRangeHeader(t, val)
		} else {
			start, end = 0, int64(size)-1
		}

		// Set up response headers
		w.Header().Set("Content-Length", fmt.Sprintf("%v", end-start+1))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)

		// Write image data
		if _, err := w.Write(sampleBytes[start : end+1]); err != nil {
			t.Fatalf("error writing response: %v", err)
		}
	}))
	return srv
}

// TestLegacyDownloadImage downloads random image data from mock library and compares hash to
// ensure download integrity.
func TestLegacyDownloadImage(t *testing.T) {
	tests := []struct {
		name                string
		multistreamDownload bool
	}{
		{"SingleStream", false},
		{"MultiStream", true},
	}

	// Total overkill seeding the random number generator
	seedRandomNumberGenerator(t)

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			// Generate random bytes to simulate file; upto 256k
			sampleBytes := generateSampleData(t)
			size := int64(len(sampleBytes))

			testLogger.Logf("Generated %v bytes of mock image data", size)

			hash := sha256.Sum256(sampleBytes)

			// Create mock library server that responds to '/version' and '/v1/imagefile' only
			srv := mockLibraryServer(t, sampleBytes, size, tt.multistreamDownload)
			defer srv.Close()

			// Initialize scs-library-client
			c, err := NewClient(&Config{BaseURL: srv.URL, Logger: testLogger})
			if err != nil {
				t.Fatalf("error initializing client: %v", err)
			}

			// Initialize sink for downloaded sample image
			dst := &inMemoryBuffer{buf: make([]byte, size)}

			err = c.legacyDownloadImage(
				context.Background(),
				"amd64",
				"entity/collection/container",
				"tag",
				dst,
				&Downloader{Concurrency: 4, PartSize: 64 * 1024},
				&NoopProgressBar{},
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Compare sha256 hash of data sent with hash of data received
			if got, want := sha256.Sum256(dst.Bytes()), hash; !reflect.DeepEqual(got, want) {
				t.Fatalf("unexpected hash: got %x, want %v", got, want)
			}
		})
	}
}
