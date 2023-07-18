// Copyright (c) 2018-2023, Sylabs Inc. All rights reserved.
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
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"

	jsonresp "github.com/sylabs/json-resp"
)

const (
	testQuotaUsageBytes int64 = 64 * 1024 * 1024
	testQuotaTotalBytes int64 = 1024 * 1024 * 1024
	testContainerURL          = "/library/entity/collection/container"
	testPayload               = "testtesttesttest"
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

func (m *v2ImageUploadMockService) MockS3PresignedURLPUTEndpoint(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	m.putCalled = true
}

func (m *v2ImageUploadMockService) MockImageFileCompleteEndpoint(w http.ResponseWriter, _ *http.Request) {
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

func mockS3Server(t *testing.T, statusCode int) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			return
		}
		w.Header().Set("ETag", "thisisasampleetag")
	}))
}

func initClient(t *testing.T, url string) *Client {
	t.Helper()

	c, err := NewClient(&Config{BaseURL: url, AuthToken: testToken, Logger: &stdLogger{}})
	if err != nil {
		t.Fatalf("error initializing client: %v", err)
	}

	return c
}

func commonHandler(t *testing.T, code int, w http.ResponseWriter) {
	t.Helper()

	if code != http.StatusOK {
		if code != http.StatusInternalServerError {
			w.WriteHeader(code)
			return
		}
		_, err := w.Write([]byte("'?"))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		return
	}
}

func uploadImageHelperHandler(t *testing.T, code int, w http.ResponseWriter) {
	t.Helper()

	if code != http.StatusOK {
		w.WriteHeader(code)
		return
	}
}

func Test_UploadImageBadPath(t *testing.T) {
	s3Server := mockS3Server(t, http.StatusOK)
	defer s3Server.Close()

	tests := []struct {
		name        string
		path        string
		expectError bool
	}{
		{"badPath", "\n", true},
		{"pathError", "library://entity/collection/container:te,st", true},
		{"test", "entity/collection/container", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.NewServeMux()
			h.HandleFunc("/v1/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				commonHandler(t, http.StatusOK, w)
			}))

			h.HandleFunc("/v1/tags/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := TagMap{"test": "testValue"}

				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			h.HandleFunc("/v1/entities/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploadImageHelperHandler(t, http.StatusOK, w)
				resp := Entity{ID: "testID"}

				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			h.HandleFunc("/v1/collections/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploadImageHelperHandler(t, http.StatusOK, w)
				resp := Collection{
					ID: "testID",
				}
				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			h.HandleFunc("/v1/containers/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploadImageHelperHandler(t, http.StatusOK, w)

				resp := Container{ID: "test"}

				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			h.HandleFunc("/v1/images/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploadImageHelperHandler(t, http.StatusOK, w)

				commonHandler(t, http.StatusOK, w)

				resp := Image{ID: "testID"}

				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			libraryServer := httptest.NewServer(h)
			defer libraryServer.Close()

			c := initClient(t, libraryServer.URL)

			r := strings.NewReader(testPayload)

			etag, err := c.UploadImage(
				context.Background(),
				r,
				tt.path,
				runtime.GOARCH,
				[]string{"tag"},
				"test",
				&defaultUploadCallback{r: r},
			)
			if (err == nil) && tt.expectError {
				t.Fatal("unexpected success")
			}

			_ = etag
		})
	}
}

type testCodesStruct struct {
	entity     int
	collection int
	container  int
	image      int
}

func Test_UploadImage(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		s3StatusCode int
		expectError  bool
		codes        testCodesStruct
	}{
		{
			"statusOK", http.StatusOK, http.StatusOK, false,
			testCodesStruct{http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK},
		},
		{
			"badRequest", http.StatusBadRequest, http.StatusBadRequest, true,
			testCodesStruct{http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK},
		},
		{
			"internalServerError", http.StatusInternalServerError, http.StatusInternalServerError, true,
			testCodesStruct{http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK},
		},
		{
			"entityNotFound", http.StatusOK, http.StatusOK, true,
			testCodesStruct{http.StatusNotFound, http.StatusOK, http.StatusOK, http.StatusOK},
		},
		{
			"entityBadRequest", http.StatusOK, http.StatusOK, true,
			testCodesStruct{http.StatusBadRequest, http.StatusOK, http.StatusOK, http.StatusOK},
		},
		{
			"collectionNotFound", http.StatusOK, http.StatusOK, true,
			testCodesStruct{http.StatusOK, http.StatusNotFound, http.StatusOK, http.StatusOK},
		},
		{
			"collectionBadRequest", http.StatusOK, http.StatusOK, true,
			testCodesStruct{http.StatusOK, http.StatusBadRequest, http.StatusOK, http.StatusOK},
		},
		{
			"containerNotFound", http.StatusOK, http.StatusOK, true,
			testCodesStruct{http.StatusOK, http.StatusOK, http.StatusNotFound, http.StatusOK},
		},
		{
			"containerBadRequest", http.StatusOK, http.StatusOK, true,
			testCodesStruct{http.StatusOK, http.StatusOK, http.StatusBadRequest, http.StatusOK},
		},
		{
			"imageNotFound", http.StatusOK, http.StatusOK, true,
			testCodesStruct{http.StatusOK, http.StatusOK, http.StatusOK, http.StatusNotFound},
		},
		{
			"imageBadRequest", http.StatusOK, http.StatusOK, true,
			testCodesStruct{http.StatusOK, http.StatusOK, http.StatusOK, http.StatusBadRequest},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3Server := mockS3Server(t, tt.s3StatusCode)
			defer s3Server.Close()

			h := http.NewServeMux()
			h.HandleFunc("/v1/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				commonHandler(t, tt.statusCode, w)
			}))

			h.HandleFunc("/v1/tags/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := TagMap{"test": "testValue"}

				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			h.HandleFunc("/v1/entities/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploadImageHelperHandler(t, tt.codes.entity, w)
				resp := Entity{ID: "testID"}

				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			h.HandleFunc("/v1/collections/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploadImageHelperHandler(t, tt.codes.collection, w)
				resp := Collection{
					ID: "testID",
				}
				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			h.HandleFunc("/v1/containers/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploadImageHelperHandler(t, tt.codes.container, w)

				resp := Container{ID: "test"}

				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			h.HandleFunc("/v1/images/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploadImageHelperHandler(t, tt.codes.image, w)

				commonHandler(t, tt.statusCode, w)

				resp := Image{ID: "testID"}

				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			libraryServer := httptest.NewServer(h)
			defer libraryServer.Close()

			c := initClient(t, libraryServer.URL)

			r := strings.NewReader(testPayload)

			etag, err := c.UploadImage(
				context.Background(),
				r,
				"entity/collection/container",
				runtime.GOARCH,
				[]string{"tag"},
				"test",
				&defaultUploadCallback{r: r},
			)
			if (err != nil) != tt.expectError {
				t.Fatalf("unexpected error: %v", err)
			}

			_ = etag
		})
	}
}

func Test_postFileWrapper(t *testing.T) {
	s3Server := mockS3Server(t, http.StatusOK)
	defer s3Server.Close()

	h := http.NewServeMux()
	h.HandleFunc("/v1/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	libraryServer := httptest.NewServer(h)
	defer libraryServer.Close()

	c := initClient(t, libraryServer.URL)

	r := strings.NewReader(testPayload)

	etag, err := c.postFileWrapper(
		context.Background(),
		r,
		int64(len(testPayload)),
		"xxx",
		&defaultUploadCallback{r: r},
		map[string]string{"sha256sum": "xxx"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = etag
}

func Test_postFileV2(t *testing.T) {
	s3Server := mockS3Server(t, http.StatusOK)
	defer s3Server.Close()

	tests := []struct {
		name        string
		size        int64
		expectError bool
	}{
		{"basic", int64(len(testPayload)), false},
		{"invalid", minimumPartSize + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.NewServeMux()
			h.HandleFunc("/v2/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.String(), "/_multipart"):
					switch r.Method {
					case http.MethodPost:
						resp := MultipartUploadStartResponse{
							Data: MultipartUpload{
								UploadID:   "xxx",
								TotalParts: 1,
								PartSize:   123,
								Options:    map[string]string{OptionS3Compliant: "true"},
							},
						}

						if err := json.NewEncoder(w).Encode(&resp); err != nil {
							t.Fatalf("Error encoding JSON response: %v", err)
						}
					case http.MethodPut:
						resp := UploadImagePartResponse{Data: UploadImagePart{PresignedURL: s3Server.URL}}

						if err := json.NewEncoder(w).Encode(&resp); err != nil {
							t.Fatalf("Error encoding JSON response: %v", err)
						}
					}
				case strings.HasSuffix(r.URL.String(), "/_multipart_complete"):
					resp := CompleteMultipartUploadResponse{Data: UploadImageComplete{}}

					if err := json.NewEncoder(w).Encode(&resp); err != nil {
						t.Fatalf("Error encoding JSON response: %v", err)
					}
				default:
					resp := UploadImageResponse{Data: UploadImage{UploadURL: s3Server.URL}}

					if err := json.NewEncoder(w).Encode(&resp); err != nil {
						t.Fatalf("Error encoding JSON response: %v", err)
					}
				}
			}))

			libraryServer := httptest.NewServer(h)
			defer libraryServer.Close()

			c := initClient(t, libraryServer.URL)

			r := strings.NewReader(testPayload)

			etag, err := c.postFileV2(
				context.Background(),
				r,
				tt.size,
				"xxx",
				&defaultUploadCallback{r: r},
				map[string]string{"sha256sum": "xxx"},
			)
			if (err != nil) != tt.expectError {
				t.Fatalf("unexpected error: %v", err)
			}

			_ = etag
		})
	}
}

func Test_postFileV2Multipart(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		s3StatusCode int
		expectError  bool
	}{
		{"statusOK", http.StatusOK, http.StatusOK, false},
		{"internalServerError", http.StatusInternalServerError, http.StatusInternalServerError, true},
		{"okBadRequest", http.StatusOK, http.StatusBadRequest, true},
		{"badRequestOK", http.StatusBadRequest, http.StatusOK, true},
		{"internalOK", http.StatusInternalServerError, http.StatusOK, true},
		{"okInternal", http.StatusOK, http.StatusInternalServerError, true},
		{"internalBadRequest", http.StatusInternalServerError, http.StatusBadRequest, true},
		{"badRequestInternal", http.StatusBadRequest, http.StatusInternalServerError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3Server := mockS3Server(t, tt.s3StatusCode)
			defer s3Server.Close()

			h := http.NewServeMux()
			h.HandleFunc("/v2/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				commonHandler(t, tt.statusCode, w)
				if strings.HasSuffix(r.URL.String(), "/_multipart") {
					switch r.Method {
					case http.MethodPost:
						resp := MultipartUploadStartResponse{
							Data: MultipartUpload{
								UploadID:   "xxx",
								TotalParts: 1,
								PartSize:   123456,
								Options:    map[string]string{OptionS3Compliant: "true"},
							},
						}

						if err := json.NewEncoder(w).Encode(&resp); err != nil {
							t.Fatalf("Error encoding JSON response: %v", err)
						}
					case http.MethodPut:
						resp := UploadImagePartResponse{Data: UploadImagePart{PresignedURL: s3Server.URL}}

						if err := json.NewEncoder(w).Encode(&resp); err != nil {
							t.Fatalf("Error encoding JSON response: %v", err)
						}
					}
				} else if strings.HasSuffix(r.URL.String(), "/_multipart_complete") {
					resp := CompleteMultipartUploadResponse{Data: UploadImageComplete{}}

					if err := json.NewEncoder(w).Encode(&resp); err != nil {
						t.Fatalf("Error encoding JSON response: %v", err)
					}
				}
			}))

			libraryServer := httptest.NewServer(h)
			defer libraryServer.Close()

			c := initClient(t, libraryServer.URL)

			r := strings.NewReader(testPayload)

			etag, err := c.postFileV2Multipart(
				context.Background(),
				r,
				int64(len(testPayload)),
				"xxx",
				&defaultUploadCallback{r: r},
			)
			if (err != nil) != tt.expectError {
				t.Fatalf("unexpected error: %v", err)
			}

			_ = etag
		})
	}
}

func Test_getPartSize(t *testing.T) {
	tests := []struct {
		name           string
		bytesRemaining int64
		partSize       int64
		want           int64
	}{
		{"moreBytesThanParts", 2, 1, 1},
		{"morePartsThanBytes", 1, 2, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := getPartSize(tt.bytesRemaining, tt.partSize), tt.want; got != want {
				t.Fatalf("got: %v, want: %v", got, want)
			}
		})
	}
}

func Test_startMultipartUpload(t *testing.T) {
	s3Server := mockS3Server(t, http.StatusOK)
	defer s3Server.Close()

	tests := []struct {
		name        string
		statusCode  int
		expectError bool
	}{
		{"success", http.StatusOK, false},
		{"badRequest", http.StatusBadRequest, true},
		{"internalServerError", http.StatusInternalServerError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.NewServeMux()
			h.HandleFunc("/v2/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				commonHandler(t, tt.statusCode, w)

				if strings.HasSuffix(r.URL.String(), "/_multipart") {
					resp := MultipartUploadStartResponse{
						Data: MultipartUpload{
							UploadID:   "testUploadID",
							TotalParts: 1,
							PartSize:   123456,
							Options:    map[string]string{OptionS3Compliant: "true"},
						},
					}

					if err := json.NewEncoder(w).Encode(&resp); err != nil {
						t.Fatalf("Error encoding JSON response: %v", err)
					}
				}
			}))

			libraryServer := httptest.NewServer(h)
			defer libraryServer.Close()

			c := initClient(t, libraryServer.URL)

			multi, err := c.startMultipartUpload(context.Background(), 0, "testID")
			if (err != nil) != tt.expectError {
				t.Fatalf("error uploading part: %v", err)
			}

			_ = multi
		})
	}
}

func Test_remoteSHA256ChecksumSupport(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"basic", "x-amz-content-sha256", true},
		{"NotMatch", "x-amz-content-sha256x", false},
		{"NoValue", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := url.Values{}

			q.Set("X-Amz-SignedHeaders", tt.value)

			u := &url.URL{RawQuery: q.Encode()}
			if got, want := remoteSHA256ChecksumSupport(u), tt.want; got != want {
				t.Fatalf("got: %v, want: %v", got, want)
			}
		})
	}
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

			c := initClient(t, m.baseURI)
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

func Test_legacyPostFileV2URL(t *testing.T) {
	s3Server := mockS3Server(t, http.StatusOK)
	defer s3Server.Close()

	tests := []struct {
		name        string
		url         string
		expectError bool
	}{
		{"basic", s3Server.URL, false},
		{"emptyURL", "", true},
		{"parseURLError", "\n", true},
		{"unsupported", "test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.NewServeMux()
			h.HandleFunc("/v2/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				commonHandler(t, http.StatusOK, w)

				resp := UploadImageResponse{Data: UploadImage{UploadURL: tt.url}}

				if err := json.NewEncoder(w).Encode(&resp); err != nil {
					t.Fatalf("Error encoding JSON response: %v", err)
				}
			}))

			libraryServer := httptest.NewServer(h)
			defer libraryServer.Close()

			c := initClient(t, libraryServer.URL)

			r := strings.NewReader(testPayload)

			etag, err := c.legacyPostFileV2(
				context.Background(),
				0,
				"xxx",
				&defaultUploadCallback{r: r},
				map[string]string{"sha256sum": "xxx"},
			)
			if (err != nil) != tt.expectError {
				t.Fatalf("unexpected error: %v", err)
			}

			_ = etag
		})
	}
}

func Test_Test_multipartUploadPartBadSize(t *testing.T) {
	s3Server := mockS3Server(t, http.StatusOK)
	defer s3Server.Close()

	h := http.NewServeMux()
	h.HandleFunc("/v2/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		commonHandler(t, http.StatusOK, w)

		if strings.HasSuffix(r.URL.String(), "/_multipart") {
			resp := UploadImagePartResponse{Data: UploadImagePart{PresignedURL: s3Server.URL}}

			if err := json.NewEncoder(w).Encode(&resp); err != nil {
				t.Fatalf("Error encoding JSON response: %v", err)
			}
		}
	}))

	libraryServer := httptest.NewServer(h)
	defer libraryServer.Close()

	c := initClient(t, libraryServer.URL)

	r := strings.NewReader(testPayload)

	etag, err := c.multipartUploadPart(
		context.Background(),
		0,
		&uploadManager{
			Source:   r,
			Size:     int64(len(testPayload)) + 1,
			ImageID:  "testImageID",
			UploadID: "testUploadID",
		},
		&defaultUploadCallback{r: r},
		true,
	)
	if err == nil {
		t.Fatal("unexpected success")
	}

	_ = etag
}

func Test_multipartUploadPart(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		s3StatusCode int
		expectError  bool
	}{
		{"statusOK", http.StatusOK, http.StatusOK, false},
		{"badRequest", http.StatusBadRequest, http.StatusBadRequest, true},
		{"internalServerError", http.StatusInternalServerError, http.StatusInternalServerError, true},
		{"okBadRequest", http.StatusOK, http.StatusBadRequest, true},
		{"badRequestOK", http.StatusBadRequest, http.StatusOK, true},
		{"internalOK", http.StatusInternalServerError, http.StatusOK, true},
		{"okInternal", http.StatusOK, http.StatusInternalServerError, true},
		{"internalBadRequest", http.StatusInternalServerError, http.StatusBadRequest, true},
		{"badRequestInternal", http.StatusBadRequest, http.StatusInternalServerError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3Server := mockS3Server(t, tt.s3StatusCode)
			defer s3Server.Close()

			h := http.NewServeMux()
			h.HandleFunc("/v2/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				commonHandler(t, tt.statusCode, w)

				if strings.HasSuffix(r.URL.String(), "/_multipart") {
					resp := UploadImagePartResponse{Data: UploadImagePart{PresignedURL: s3Server.URL}}

					if err := json.NewEncoder(w).Encode(&resp); err != nil {
						t.Fatalf("Error encoding JSON response: %v", err)
					}
				}
			}))

			libraryServer := httptest.NewServer(h)
			defer libraryServer.Close()

			c := initClient(t, libraryServer.URL)

			r := strings.NewReader(testPayload)

			etag, err := c.multipartUploadPart(
				context.Background(),
				0,
				&uploadManager{
					Source:   r,
					Size:     int64(len(testPayload)),
					ImageID:  "testImageID",
					UploadID: "testUploadID",
				},
				&defaultUploadCallback{r: r},
				true,
			)
			if (err != nil) != tt.expectError {
				t.Fatalf("error uploading part: %v", err)
			}

			_ = etag
		})
	}
}

func Test_completeMultipartUpload(t *testing.T) {
	s3Server := mockS3Server(t, http.StatusOK)
	defer s3Server.Close()

	tests := []struct {
		name        string
		statusCode  int
		expectError bool
	}{
		{"statusOK", http.StatusOK, false},
		{"badRequest", http.StatusBadRequest, true},
		{"internalServerError", http.StatusInternalServerError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.NewServeMux()
			h.HandleFunc("/v2/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				commonHandler(t, tt.statusCode, w)

				if strings.HasSuffix(r.URL.String(), "/_multipart_complete") {
					resp := CompleteMultipartUploadResponse{Data: UploadImageComplete{ContainerURL: s3Server.URL}}

					if err := json.NewEncoder(w).Encode(&resp); err != nil {
						t.Fatalf("Error encoding JSON response: %v", err)
					}
				}
			}))

			libraryServer := httptest.NewServer(h)
			defer libraryServer.Close()

			c := initClient(t, libraryServer.URL)

			etag, err := c.completeMultipartUpload(
				context.Background(),
				&[]CompletedPart{{PartNumber: 0, Token: "xxx"}},
				&uploadManager{
					Source:   strings.NewReader(testPayload),
					Size:     int64(len(testPayload)),
					ImageID:  "testImageID",
					UploadID: "testUploadID",
				})
			if (err != nil) != tt.expectError {
				t.Fatalf("error uploading part: %v", err)
			}

			_ = etag
		})
	}
}

func Test_abortMultipartUpload(t *testing.T) {
	s3Server := mockS3Server(t, http.StatusOK)
	defer s3Server.Close()

	tests := []struct {
		name        string
		statusCode  int
		expectError bool
	}{
		{"statusOK", http.StatusOK, false},
		{"badRequest", http.StatusBadRequest, true},
		{"internalServerError", http.StatusInternalServerError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.NewServeMux()
			h.HandleFunc("/v2/imagefile/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploadImageHelperHandler(t, tt.statusCode, w)
			}))

			libraryServer := httptest.NewServer(h)
			defer libraryServer.Close()

			c := initClient(t, libraryServer.URL)

			err := c.abortMultipartUpload(
				context.Background(),
				&uploadManager{
					Source:   strings.NewReader(testPayload),
					Size:     int64(len(testPayload)),
					ImageID:  "testImageID",
					UploadID: "testUploadID",
				})
			if (err != nil) != tt.expectError {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
