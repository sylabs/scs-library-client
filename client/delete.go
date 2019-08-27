package client

import (
	"context"
	"errors"
	"fmt"
)

// DeleteImage deletes requested image.
func (c *Client) DeleteImage(ctx context.Context, image, tag string) error {
	if image == "" || tag == "" {
		return errors.New("image and tag are required")
	}

	path := fmt.Sprintf("/v1/images/%s:%s", image, tag)
	_, err := c.doDeleteRequest(ctx, path)
	return err
}
