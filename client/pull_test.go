// Copyright (c) 2018-2023, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	crypto_rand "crypto/rand"

	math_rand "math/rand"
)

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

func generateSampleData(t *testing.T, size int64) []byte {
	t.Helper()

	const maxSampleDataSize = 1 * 1024 * 1024 // 1 MiB

	if size == -1 {
		size = math_rand.Int63() % maxSampleDataSize
	}

	sampleBytes := make([]byte, size)

	if _, err := crypto_rand.Read(sampleBytes); err != nil {
		t.Fatalf("error generating random bytes: %v", err)
	}

	return sampleBytes
}

// mockLibraryServer returns *httptest.Server that mocks Cloud Library server; in particular,
// it has handlers for /version, /v1/images, /v1/imagefile, and /v1/imagepart
func mockLibraryServer(t *testing.T, data []byte, multistream bool) *httptest.Server {
	size := int64(len(data))

	mux := http.NewServeMux()

	mux.HandleFunc("/version", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte("{\"data\": {\"apiVersion\": \"1.0.0\"}}")); err != nil {
			t.Fatalf("error writing /version response: %v", err)
		}
	}))

	if multistream {
		mux.HandleFunc("/v1/images/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)

			if _, err := w.Write([]byte(fmt.Sprintf("{\"data\": {\"size\": %v}}", size))); err != nil {
				t.Fatalf("error writing /v1/images response: %v", err)
			}
		}))

		mux.HandleFunc("/v1/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			redirectURL := &url.URL{
				Scheme: "http",
				Host:   r.Host,
				Path:   "/v1/imagepart/" + strings.TrimPrefix(r.URL.Path, "/v1/imagefile/"),
			}
			w.Header().Set("Location", redirectURL.String())
			w.WriteHeader(http.StatusSeeOther)
		}))

		mux.HandleFunc("/v1/imagepart/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle Range request for multipart downloads
			var start, end int64
			if val := r.Header.Get("Range"); val != "" {
				start, end = parseRangeHeader(t, val)

				if end < 0 || start < 0 || start > end {
					w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
					return
				}
			} else {
				t.Fatal("Missing HTTP Range header")
			}

			if end-start+1 > size {
				// Size of requested range is larger than available data, adjust end accordingly
				end = size - 1
			}

			writeBlob(t, data, size, start, end, http.StatusPartialContent, w)
		}))
	} else {
		// single stream
		mux.HandleFunc("/v1/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeBlob(t, data, size, 0, size-1, http.StatusOK, w)
		}))
	}

	mux.HandleFunc("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("Unhandled HTTP request: method=[%v], path=[%v]", r.Method, r.URL.Path)
	}))

	return httptest.NewServer(mux)
}

func writeBlob(t *testing.T, buf []byte, size, start, end int64, code int, w http.ResponseWriter) {
	t.Helper()

	// contentLength varies depending if Range request is being made
	contentLength := size

	// Set up response headers
	w.Header().Set("Content-Type", "application/octet-stream")

	// Ensure 'Content-Range' header is included if HTTP status is 206 (Partial Content)
	if code == http.StatusPartialContent {
		// Reports range length *not* content length
		contentLength = end - start + 1

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	}

	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))

	w.WriteHeader(code)

	// Write image data
	if _, err := w.Write(buf[start : end+1]); err != nil {
		t.Fatalf("error writing response: %v", err)
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
		{"UpperAndLowercase", "http://TESTHOST", "http://testhost", true},
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

// TestLibraryDownloadImage downloads random image data from mock library and compares hash to
// ensure download integrity.
func TestLibraryDownloadImage(t *testing.T) {
	tests := []struct {
		name                string
		spec                *Downloader
		multistreamDownload bool
		size                int64
	}{
		{"SingleStream", DefaultDownloadSpec, false, -1},
		{"SingleStreamPartSizeMatchesImageSize", &Downloader{Concurrency: 4, PartSize: 64 * 1024}, false, 64 * 1024},
		{"SingleStreamPartSizeGreaterThanImageSize", &Downloader{Concurrency: 4, PartSize: 64 * 1024}, false, 64*1024 - 1},
		{"SingleStreamPartSizeLessThanImageSize", &Downloader{Concurrency: 4, PartSize: 64*1024 - 1}, false, 64 * 1024},
		{"MultiStream", DefaultDownloadSpec, true, -1},
		{"MultiStreamPartSizeEqualsImageSize", &Downloader{Concurrency: 4, PartSize: 64 * 1024}, true, 64 * 1024},
		{"MultiStreamPartSizeGreaterThanImageSize", &Downloader{Concurrency: 4, PartSize: 64 * 1024}, true, 64*1024 - 1},
		{"MultiStreamPartSizeLessThanImageSize", &Downloader{Concurrency: 4, PartSize: 64*1024 - 1}, true, 64 * 1024},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			// Generate random bytes; if tt.size == -1, generate random number of bytes
			imageData := generateSampleData(t, tt.size)
			size := int64(len(imageData))

			testLogger.Logf("Generated %d bytes of mock image data", size)

			// Calculate hash of sample image data used to compare with what was actually downloaded
			hash := sha256.Sum256(imageData)

			// Create mock library server that responds to '/version' and '/v1/imagefile' only
			srv := mockLibraryServer(t, imageData, tt.multistreamDownload)
			defer srv.Close()

			// Initialize scs-library-client
			c, err := NewClient(&Config{BaseURL: srv.URL, Logger: testLogger})
			if err != nil {
				t.Fatalf("error initializing client: %v", err)
			}

			// Initialize sink for downloaded sample image
			dst := &inMemoryBuffer{buf: make([]byte, size)}

			err = c.libraryDownloadImage(
				context.Background(),
				"amd64",
				"entity/collection/container",
				"tag",
				dst,
				tt.spec,
				&NoopProgressBar{},
			)
			if err != nil {
				t.Fatal(err)
			}

			// Compare sha256 hash of data sent with hash of data received
			if got, want := sha256.Sum256(dst.Bytes()), hash; !reflect.DeepEqual(got, want) {
				t.Fatalf("unexpected hash: got %x, want %v", got, want)
			}
		})
	}
}
