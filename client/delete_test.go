package client

import (
	"context"
	"fmt"
	"testing"
)

func Test_DeleteImage(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		image       string
		tag         string
		expectError bool
		code        int
	}{
		{
			image:       "",
			tag:         "",
			expectError: true,
		},
		{
			image:       "test",
			tag:         "",
			expectError: true,
		},
		{
			image:       "",
			tag:         "v0.0.1",
			expectError: true,
		},
		{
			image: "test",
			tag:   "v0.0.1",
			code:  200,
		},
	}

	for _, tt := range tests {
		m := mockService{
			t:        t,
			code:     tt.code,
			httpPath: fmt.Sprintf("/v1/images/%s:%s", tt.image, tt.tag),
		}
		m.Run()
		defer m.Stop()

		c, err := NewClient(&Config{BaseURL: m.baseURI})
		if err != nil {
			t.Errorf("Error initializing client: %v", err)
		}

		err = c.DeleteImage(ctx, tt.image, tt.tag)
		if !tt.expectError && err != nil {
			t.Errorf("Unexpected err: %s", err)
		}
	}
}
