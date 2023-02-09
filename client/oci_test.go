// Copyright (c) 2022-2023, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOciRegistryAuth(t *testing.T) {
	const ociRegistryURI = "https://registry"

	tests := []struct {
		name                       string
		directOciDownloadSupported bool
	}{
		{"Basic", true},
		{"NotSupported", false},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testShimSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !tt.directOciDownloadSupported {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				response := struct {
					Token       string `json:"token"`
					RegistryURI string `json:"url"`
					Name        string `json:"name"`
				}{
					Token:       "xxx",
					RegistryURI: ociRegistryURI,
				}

				if v := r.URL.Query().Get("namespace"); v != "" {
					response.Name = v
				} else {
					t.Fatal("Query string \"namespace\" not set")
				}

				if v := r.URL.Query().Get("accessTypes"); v == "" {
					t.Fatalf("Query string \"accessTypes\" not set")
				}

				if err := json.NewEncoder(w).Encode(&response); err != nil {
					t.Fatalf("error JSON encoding: %v", err)
				}
			}))
			defer testShimSrv.Close()

			c, err := NewClient(&Config{
				BaseURL:   testShimSrv.URL,
				Logger:    &stdLogger{},
				UserAgent: "scs-library-client-unit-tests/1.0",
			})
			if err != nil {
				t.Fatalf("error initializing client: %v", err)
			}

			u, creds, name, err := c.ociRegistryAuth(context.Background(), "testproject/testrepo", []accessType{accessTypePull})
			if tt.directOciDownloadSupported && err != nil {
				t.Fatalf("error getting OCI registry credentials: %v", err)
			} else if !tt.directOciDownloadSupported && err == nil {
				t.Fatal("unexpected success")
			}

			if tt.directOciDownloadSupported {
				if got, want := name, "testproject/testrepo"; got != want {
					t.Fatalf("unexpected OCI artifact name: got %v, want %v", got, want)
				}

				if got, want := u.String(), ociRegistryURI; got != want {
					t.Fatalf("unexpected OCI registry URI: got %v, want %v", got, want)
				}

				if creds == nil {
					t.Fatal("expecting bearer token credential")
				}
			}
		})
	}
}