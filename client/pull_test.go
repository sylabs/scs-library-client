// Copyright (c) 2018-2023, Sylabs Inc. All rights reserved.
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
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
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

				start, end, err := parseRangeHeader(t, r.Header.Get("Range"))
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if got, want := start, tt.rangeStart; got != want {
					t.Fatalf("Unexpected range start: got %d, want %d", got, want)
				}

				if got, want := end, tt.rangeEnd; got != want {
					t.Fatalf("Unexpected range end: got %d, want %d", got, want)
				}

				authorizationHeaderPresent = parseAuthorizationHeader(t, r.Header.Get("Authorization"), tt.authToken)
			}))
			defer cloudLibrarySrv.Close()

			// fileSrv mocks an alternate server than test cloud library server
			fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				start, end, err := parseRangeHeader(t, r.Header.Get("Range"))
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if got, want := start, tt.rangeStart; got != want {
					t.Fatalf("Unexpected range start: got %d, want %d", got, want)
				}

				if got, want := end, tt.rangeEnd; got != want {
					t.Fatalf("Unexpected range end: got %d, want %d", got, want)
				}

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

			res, err := c.httpGetRangeRequest(context.Background(), endpoint, tt.authToken, tt.rangeStart, tt.rangeEnd)
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

type debugLogger struct{}

func (logger *debugLogger) Log(args ...interface{}) {
	log.Print(args...)
}

func (logger *debugLogger) Logf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

var (
	// Version data from Enterprise library v1.x
	entVersion1x = "{\"data\": {\"apiVersion\": \"2.0.0-alpha.2\", \"version\": \"v1.3.4+1-0-g20da0ec\"}}}"

	// Version data from Enterprise library 2.x
	entVersion2x = "{\"data\": {\"apiVersion\": \"1.0.0\", \"version\": \"v0.3.4+1-0-gbbc7c9c\"}}}"
)

func mockLibraryServer(t *testing.T, sampleData []byte, libraryVersion, redirectHost string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		if _, err := w.Write([]byte(libraryVersion)); err != nil {
			t.Fatalf("writing /version response: %v", err)
		}
	})
	mux.HandleFunc("/v1/imagefile/", func(w http.ResponseWriter, r *http.Request) {
		var redirectURL *url.URL

		if redirectHost == "" {
			// If redirectHost is undefined, redirect to same URL, different endpoint
			redirectURL = &url.URL{
				Scheme:   "http",
				Host:     r.Host,
				Path:     "/v1/imagepart" + strings.TrimPrefix(r.URL.Path, "/v1/imagefile"),
				RawQuery: r.URL.RawQuery,
			}
		} else {
			// Redirect to specified external URL
			u, err := url.Parse(redirectHost)
			if err != nil {
				t.Fatalf("parsing redirect URL %v: %v", redirectURL, err)
			}

			u.Path = "/filepath"

			redirectURL = u
		}

		(&debugLogger{}).Logf("Redirect URL: %v", redirectURL.String())

		http.Redirect(w, r, redirectURL.String(), http.StatusSeeOther)
	})

	mux.HandleFunc("/v1/imagepart/", func(w http.ResponseWriter, r *http.Request) {
		imagePartHandler(t, sampleData, true, w, r)
	})

	return httptest.NewServer(mux)
}

// TestConcurrentDownloadImage tests concurrent download for library 1.x and 2.x to an "internal"
// URL (same host as library server)
func TestConcurrentDownloadImage(t *testing.T) {
	logger := &debugLogger{}

	tests := []struct {
		name           string
		libraryVersion string
	}{
		{"LibraryV1", entVersion1x},
		{"LibraryV2", entVersion2x},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampleData := generateSampleData(t)
			sampleDataChecksum := sha256.Sum256(sampleData)

			logger.Logf("Using %d byte(s) of sample data", len(sampleData))

			srv := mockLibraryServer(t, sampleData, tt.libraryVersion, "")
			defer srv.Close()

			logger.Logf("Mock library URL: %v", srv.URL)

			c, err := NewClient(&Config{BaseURL: srv.URL, AuthToken: "xxxxx", Logger: logger})
			if err != nil {
				t.Fatalf("Error initializing client: %v", err)
			}

			fp, err := os.CreateTemp("", "download-unit-test-*")
			if err != nil {
				t.Fatalf("Error creating temporary file: %v", err)
			}
			defer func() {
				fp.Close()

				if err := os.Remove(fp.Name()); err != nil {
					logger.Logf("Error removing temporary file %v: %v", fp.Name(), err)
				}

				logger.Logf("Temporary file %v removed", fp.Name())
			}()

			logger.Logf("Using temporary file %v", fp.Name())

			if err := c.ConcurrentDownloadImage(
				context.Background(),
				fp,
				"amd64",
				"entity/collection/container",
				"latest",
				&Downloader{Concurrency: 4, PartSize: 1024 * 1024, BufferSize: 32768},
				nil,
			); err != nil {
				t.Fatal(err.Error())
			}

			_ = fp.Close()

			resultChecksum := getFileHash(t, fp.Name())

			if !bytes.Equal(sampleDataChecksum[:], resultChecksum) {
				t.Fatalf("sha256 checksum mismatch")
			}
		})
	}
}

func getFileHash(t *testing.T, filename string) []byte {
	t.Helper()

	h := sha256.New()

	fp, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Error opening file %v for reading: %v", filename, err)
	}
	if _, err := io.Copy(h, fp); err != nil {
		t.Fatalf("Error reading file %v: %v", filename, err)
	}
	defer func() {
		_ = fp.Close()
	}()

	return h.Sum(nil)
}

// TestConcurrentDownloadImageExternalRedirect tests both library 1.x and 2.x with redirect to
// an external URL
func TestConcurrentDownloadImageExternalRedirect(t *testing.T) {
	logger := &debugLogger{}

	tests := []struct {
		name           string
		libraryVersion string
	}{
		{"LibraryV1", entVersion1x},
		{"LibraryV2", entVersion2x},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampleData := generateSampleData(t)
			sampleDataChecksum := sha256.Sum256(sampleData)

			logger.Logf("Using %d byte(s) of sample data", len(sampleData))

			mux := http.NewServeMux()
			mux.HandleFunc("/filepath", func(w http.ResponseWriter, r *http.Request) {
				imagePartHandler(t, sampleData, true, w, r)
			})
			fileSrv := httptest.NewServer(mux)

			srv := mockLibraryServer(t, sampleData, tt.libraryVersion, fileSrv.URL)
			defer srv.Close()

			logger.Logf("Mock library URL: %v", srv.URL)

			c, err := NewClient(&Config{BaseURL: srv.URL, Logger: logger})
			if err != nil {
				t.Fatalf("Error initializing client: %v", err)
			}

			fp, err := os.CreateTemp("", "download-unit-test")
			if err != nil {
				t.Fatalf("Error creating temporary file: %v", err)
			}
			defer func() {
				_ = fp.Close()

				if err := os.Remove(fp.Name()); err != nil {
					logger.Logf("Error removing temporary file %v: %v", fp.Name(), err)
				}

				logger.Logf("Temporary file %v removed", fp.Name())
			}()

			logger.Logf("Using temporary file %v", fp.Name())

			if err := c.ConcurrentDownloadImage(
				context.Background(),
				fp,
				"amd64",
				"entity/collection/container",
				"latest",
				&Downloader{Concurrency: 4, PartSize: 1 * 1024 * 1024, BufferSize: 32768},
				nil,
			); err != nil {
				t.Fatal(err.Error())
			}

			resultChecksum := getFileHash(t, fp.Name())

			if !bytes.Equal(sampleDataChecksum[:], resultChecksum) {
				t.Fatalf("sha256 checksum mismatch")
			}
		})
	}
}

func imagePartHandler(t *testing.T, sampleData []byte, rangeSupport bool, w http.ResponseWriter, r *http.Request) {
	contentLength := int64(len(sampleData))

	rangeStart, rangeEnd, err := parseRangeHeader(t, r.Header.Get("Range"))
	if err != nil {
		t.Fatal(err)
	}

	// Default HTTP status is 200 (OK). This is the status code if the server does not support
	// range requests.
	code := http.StatusOK

	if !rangeSupport {
		// Range support is disabled
		rangeStart, rangeEnd = 0, contentLength-1
	} else {
		if rangeEnd-rangeStart+1 > contentLength {
			rangeEnd = contentLength - 1
		}

		if rangeStart > contentLength-1 || (rangeStart >= rangeEnd) || (rangeEnd-rangeStart+1 > contentLength) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, len(sampleData)))

		code = http.StatusPartialContent
	}

	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("Content-Type", "application/octet-stream")

	w.WriteHeader(code)

	if r.Method == http.MethodHead {
		// Don't send data on a HEAD request
		return
	}

	if _, err := w.Write(sampleData[rangeStart : rangeEnd+1]); err != nil {
		t.Fatalf("Error writing HTTP response: %v", err)
	}
}

// parseRangeHeader parses simple "Range: bytes=<start>-<end>" HTTP header and returns start/end
//
// This parser function does NOT handle multiple ranges specified in request.
func parseRangeHeader(t *testing.T, v string) (int64, int64, error) {
	t.Helper()

	if v == "" {
		return -1, -1, nil
	}

	var rangeStart int64 = -1
	var rangeEnd int64 = -1

	for _, rangeElements := range strings.Split(v, ",") {
		elements := strings.Split(rangeElements, "=")
		if elements[0] != "bytes" {
			return rangeStart, rangeEnd, fmt.Errorf("unsupported Range header arguments (\"%v\")", v)
		}
		byteRange := strings.SplitN(elements[1], "-", 2)

		value, err := strconv.ParseInt(byteRange[0], 10, 64)
		if err != nil {
			return rangeStart, rangeEnd, fmt.Errorf("malformed Range header (\"%v\"): %v", v, err)
		}
		rangeStart = value

		if len(byteRange) > 1 {
			if byteRange[1] != "*" {
				value, err := strconv.ParseInt(byteRange[1], 10, 64)
				if err != nil {
					return rangeStart, rangeEnd, fmt.Errorf("malformed Range header (\"%v\"): %v", v, err)
				}
				rangeEnd = value
			}
		}
	}

	return rangeStart, rangeEnd, nil
}

// generateSampleData generates between 0 and 16 MiB of random data
func generateSampleData(t *testing.T) []byte {
	t.Helper()

	const maxSampleDataSize = 16 * 1024 * 1024 // 16 MiB

	size := math_rand.Int63() % maxSampleDataSize

	sampleBytes := make([]byte, size)

	if _, err := crypto_rand.Read(sampleBytes); err != nil {
		t.Fatalf("error generating random bytes: %v", err)
	}

	return sampleBytes
}

func seedRandomNumberGenerator() {
	var b [8]byte
	if _, err := crypto_rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("error seeding random number generator: %v", err))
	}
	math_rand.Seed(int64(binary.LittleEndian.Uint64(b[:])))
}

func TestMain(m *testing.M) {
	seedRandomNumberGenerator()

	os.Exit(m.Run())
}
