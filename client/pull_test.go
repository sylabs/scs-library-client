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

// mockLibraryServer returns *httptest.Server that mocks Cloud Library server; in particular,
// it has handlers for /version, /v1/images, /v1/imagefile, and /v1/imagepart
func mockLibraryServer(t *testing.T, sampleBytes []byte, size int64, multistream bool) *httptest.Server {
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
			} else {
				t.Fatal("Missing HTTP Range header")
			}

			writeBlob(t, sampleBytes, start, end, http.StatusPartialContent, w)
		}))
	} else {
		// single stream
		mux.HandleFunc("/v1/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeBlob(t, sampleBytes, 0, size-1, http.StatusOK, w)
		}))
	}

	mux.HandleFunc("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("Unhandled HTTP request: method=[%v], path=[%v]", r.Method, r.URL.Path)
	}))

	return httptest.NewServer(mux)
}

func writeBlob(t *testing.T, sampleBytes []byte, start, end int64, code int, w http.ResponseWriter) {
	t.Helper()

	// Set up response headers
	w.Header().Set("Content-Length", fmt.Sprintf("%v", end-start+1))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(code)

	// Write image data
	if _, err := w.Write(sampleBytes[start : end+1]); err != nil {
		t.Fatalf("error writing response: %v", err)
	}
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
