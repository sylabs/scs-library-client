// Copyright (c) 2018-2022, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type inMemoryBuffer struct {
	m   sync.Mutex
	buf []byte
}

func (f *inMemoryBuffer) WriteAt(p []byte, ofs int64) (n int, err error) {
	f.m.Lock()
	defer f.m.Unlock()

	n = copy(f.buf[ofs:], p)
	return
}

func (f *inMemoryBuffer) Bytes() []byte {
	f.m.Lock()
	defer f.m.Unlock()

	return f.buf
}

type stdLogger struct{}

func (l *stdLogger) Log(v ...interface{}) {
	log.Print(v...)
}

func (l *stdLogger) Logf(f string, v ...interface{}) {
	log.Printf(f, v...)
}

func parseRangeHeader(t *testing.T, val string) (int64, int64) {
	t.Helper()

	if val == "" {
		return 0, 0
	}

	var start, end int64

	e := strings.SplitN(val, "=", 2)

	byteRange := strings.Split(e[1], "-")

	start, _ = strconv.ParseInt(byteRange[0], 10, 0)
	end, _ = strconv.ParseInt(byteRange[1], 10, 0)

	return start, end
}

const (
	basicAuthUsername = "user"
	basicAuthPassword = "password"
)

var (
	testLogger = &stdLogger{}
	creds      = &basicCredentials{username: basicAuthUsername, password: basicAuthPassword}
)

func TestMultistreamDownloader(t *testing.T) {
	const src = "123456789012345678901234567890"
	size := int64(len(src))

	defaultSpec := &Downloader{Concurrency: 10, PartSize: 3}

	tests := []struct {
		name      string
		size      int64
		spec      *Downloader
		expectErr bool
	}{
		{"Basic", size, defaultSpec, false},
		{"WithoutSize", 0, defaultSpec, true},
		{"SingleStream", size, &Downloader{Concurrency: 1, PartSize: 1}, false},
		{"SingleStreamWithoutSize", 0, &Downloader{Concurrency: 1, PartSize: 1}, true},
		{"ManyStreams", size, &Downloader{Concurrency: uint(size), PartSize: 1}, false},
		{"ManyStreamsWithoutSize", 0, &Downloader{Concurrency: uint(size), PartSize: 1}, true},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test http server for serving "file"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				start, end := parseRangeHeader(t, r.Header.Get("Range"))

				if username, password, ok := r.BasicAuth(); ok {
					if got, want := username, basicAuthUsername; got != want {
						t.Fatalf("unexpected basic auth username: got %v, want %v", got, want)
					}
					if got, want := password, basicAuthPassword; got != want {
						t.Fatalf("unexpected basic auth password: got %v, want %v", got, want)
					}
				}

				w.Header().Set("Content-Range", fmt.Sprintf("bytes %v-%v/%v", start, end+1, size))
				w.Header().Set("Content-Length", fmt.Sprintf("%v", end-start+1))

				w.WriteHeader(http.StatusPartialContent)

				if _, err := io.Copy(w, bytes.NewReader([]byte(src[start:end+1]))); err != nil {
					t.Fatalf("unexpected error writing http response: %v", err)
				}
			}))
			defer srv.Close()

			c, err := NewClient(&Config{Logger: testLogger})
			if err != nil {
				t.Fatalf("error initializing client: %v", err)
			}

			// Preallocate sink for downloaded file
			dst := &inMemoryBuffer{buf: make([]byte, size)}

			// Start download
			err = c.multipartDownload(context.Background(), srv.URL, creds, dst, tt.size, tt.spec, &NoopProgressBar{})
			if tt.expectErr && err == nil {
				t.Fatal("unexpected success")
			}
			if !tt.expectErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil {
				return
			}

			// Compare results with expectations
			if got, want := string(dst.Bytes()), src; got != want {
				t.Fatalf("unexpected data: got %v, want %v", got, want)
			}
		})
	}
}
