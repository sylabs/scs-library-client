package client

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func Test_DeleteImage(t *testing.T) {
	tests := []struct {
		name        string
		imageRef    string
		arch        string
		expectError bool
		code        int
		callback    func(*http.Request, *testing.T)
	}{
		{
			name:        "MissingImageRefAndArch",
			expectError: true,
		},
		{
			name:        "MissingImageRef",
			arch:        archIntel,
			expectError: true,
		},
		{
			name:        "MissingArch",
			imageRef:    "test:v0.0.1",
			expectError: true,
		},
		{
			name:     "ValidateQueryStringArch",
			imageRef: "test",
			arch:     archIntel,
			code:     http.StatusOK,
			callback: func(r *http.Request, t *testing.T) {
				// ensure arch specified in query string is as expected
				queryArch := r.URL.Query().Get("arch")
				if queryArch != archIntel {
					t.Errorf("got arch %v, want %v", queryArch, archIntel)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				httpPath:    fmt.Sprintf("/v1/images/%s", tt.imageRef),
				reqCallback: tt.callback,
			}
			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			err = c.DeleteImage(context.Background(), tt.imageRef, tt.arch)
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected err: %s", err)
			}
		})
	}
}
