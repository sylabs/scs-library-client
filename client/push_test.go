// Copyright (c) 2018-2022, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	jsonresp "github.com/sylabs/json-resp"
)

const (
	testQuotaUsageBytes int64 = 64 * 1024 * 1024
	testQuotaTotalBytes int64 = 1024 * 1024 * 1024
	testContainerURL          = "/library/entity/collection/container"
)

func Test_postFile(t *testing.T) {
	tests := []struct {
		description string
		imageRef    string
		testFile    string
		code        int
		reqCallback func(*http.Request, *testing.T)
		expectError bool
	}{
		{
			description: "Container not found response",
			code:        404,
			reqCallback: nil,
			imageRef:    "5cb9c34d7d960d82f5f5bc55",
			testFile:    "test_data/test_sha256",
			expectError: true,
		},
		{
			description: "Unauthorized response",
			code:        401,
			reqCallback: nil,
			imageRef:    "5cb9c34d7d960d82f5f5bc56",
			testFile:    "test_data/test_sha256",
			expectError: true,
		},
		{
			description: "Valid Response",
			code:        200,
			reqCallback: nil,
			imageRef:    "5cb9c34d7d960d82f5f5bc57",
			testFile:    "test_data/test_sha256",
			expectError: false,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:        t,
				code:     tt.code,
				httpPath: "/v1/imagefile/" + tt.imageRef,
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			f, err := os.Open(tt.testFile)
			if err != nil {
				t.Errorf("Error opening file %s for reading: %v", tt.testFile, err)
			}
			defer f.Close()

			fi, err := f.Stat()
			if err != nil {
				t.Errorf("Error stats for file %s: %v", tt.testFile, err)
			}
			fileSize := fi.Size()

			callback := &defaultUploadCallback{r: f}

			_, err = c.postFile(context.Background(), fileSize, tt.imageRef, callback)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
		})
	}
}

type v2ImageUploadMockService struct {
	t              *testing.T
	httpAddr       string
	httpServer     *httptest.Server
	baseURI        string
	initCalled     bool
	putCalled      bool
	completeCalled bool
}

func (m *v2ImageUploadMockService) Run() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/imagefile/5cb9c34d7d960d82f5f5bc55", m.MockImageFileEndpoint)
	mux.HandleFunc("/fake/s3/endpoint", m.MockS3PresignedURLPUTEndpoint)
	mux.HandleFunc("/v2/imagefile/5cb9c34d7d960d82f5f5bc55/_complete", m.MockImageFileCompleteEndpoint)
	m.httpServer = httptest.NewServer(mux)
	m.httpAddr = m.httpServer.Listener.Addr().String()
	m.baseURI = "http://" + m.httpAddr
}

func (m *v2ImageUploadMockService) Stop() {
	m.httpServer.Close()
}

func (m *v2ImageUploadMockService) MockImageFileEndpoint(w http.ResponseWriter, r *http.Request) {
	var uploadImageRequest UploadImageRequest
	if err := json.NewDecoder(r.Body).Decode(&uploadImageRequest); err != nil {
		if err := jsonresp.WriteError(w, "Provided image could not be decoded", http.StatusBadRequest); err != nil {
			m.t.Fatalf("error encoding error response: %v", err)
		}
	}

	// this is a bit of a nonsense assertion. All we're trying to do is confirm
	// that the sha256 checksum provided by the client is present in the
	// request. There is no actual validation of the sha256 checksum of the
	// payload in the PUT request.
	const expectedSha256 = "d7d356079af905c04e5ae10711ecf3f5b34385e9b143c5d9ddbf740665ce2fb7"
	if got, want := uploadImageRequest.SHA256Checksum, expectedSha256; got != want {
		m.t.Errorf("got checksum %v, want %v", got, want)
	}

	response := UploadImage{
		UploadURL: m.baseURI + "/fake/s3/endpoint?key=value",
	}

	err := jsonresp.WriteResponse(w, &response, http.StatusOK)
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}

	m.initCalled = true
}

func (m *v2ImageUploadMockService) MockS3PresignedURLPUTEndpoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	m.putCalled = true
}

func (m *v2ImageUploadMockService) MockImageFileCompleteEndpoint(w http.ResponseWriter, r *http.Request) {
	response := UploadImageComplete{
		Quota: QuotaResponse{
			QuotaTotalBytes: testQuotaTotalBytes,
			QuotaUsageBytes: testQuotaUsageBytes,
		},
		ContainerURL: testContainerURL,
	}

	if err := jsonresp.WriteResponse(w, &response, http.StatusOK); err != nil {
		fmt.Printf("error: %v\n", err)
	}

	m.completeCalled = true
}

func Test_legacyPostFileV2(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		testFile string
	}{
		{
			name:     "Basic",
			imageRef: "5cb9c34d7d960d82f5f5bc55",
			testFile: "test_data/test_sha256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := v2ImageUploadMockService{
				t: t,
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}
			f, err := os.Open(tt.testFile)
			if err != nil {
				t.Errorf("Error opening file %s for reading: %v", tt.testFile, err)
			}
			defer f.Close()

			fi, err := f.Stat()
			if err != nil {
				t.Errorf("Error stats for file %s: %v", tt.testFile, err)
			}
			fileSize := fi.Size()

			// calculate sha256 checksum
			sha256checksum, _, err := sha256sum(f)
			if err != nil {
				t.Fatalf("error calculating sha256 checksum: %v", err)
			}

			_, err = f.Seek(0, 0)
			if err != nil {
				t.Fatalf("unexpected error seeking in sample data file: %v", err)
			}

			callback := &defaultUploadCallback{r: f}

			// include sha256 checksum in metadata
			resp, err := c.legacyPostFileV2(context.Background(), fileSize, tt.imageRef, callback, map[string]string{
				"sha256sum": sha256checksum,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got, want := resp.Quota.QuotaUsageBytes, testQuotaUsageBytes; got != want {
				t.Errorf("got quota usage %v, want %v", got, want)
			}

			if got, want := resp.Quota.QuotaTotalBytes, testQuotaTotalBytes; got != want {
				t.Errorf("got quota total %v, want %v", got, want)
			}

			if got, want := resp.ContainerURL, testContainerURL; got != want {
				t.Errorf("got container URL %v, want %v", got, want)
			}

			if !m.initCalled {
				t.Errorf("init image upload request was not made")
			}

			if !m.putCalled {
				t.Errorf("file PUT request was not made")
			}

			if !m.completeCalled {
				t.Errorf("image upload complete request was not made")
			}
		})
	}
}
