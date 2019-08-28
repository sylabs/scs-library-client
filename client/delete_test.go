package client

import (
	"context"
	"fmt"
	"testing"
)

func Test_DeleteImage(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		imageRef    string
		arch        string
		expectError bool
		code        int
	}{
		{
			imageRef:    "",
			arch:        "",
			expectError: true,
		},
		{
			imageRef:    "test:v0.0.1",
			arch:        "",
			expectError: true,
		},
		{
			imageRef: "test",
			arch:     "1",
			code:     200,
		},
	}

	for _, tt := range tests {
		m := mockService{
			t:        t,
			code:     tt.code,
			httpPath: fmt.Sprintf("/v1/images/%s", tt.imageRef),
		}
		m.Run()
		defer m.Stop()
		c, err := NewClient(&Config{BaseURL: m.baseURI})
		if err != nil {
			t.Errorf("Error initializing client: %v", err)
		}

		err = c.DeleteImage(ctx, tt.imageRef, tt.arch)
		if !tt.expectError && err != nil {
			t.Errorf("Unexpected err: %s", err)
		}
	}
}
