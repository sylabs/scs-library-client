package client

import (
	"context"
	"errors"
	"fmt"
)

// DeleteImage deletes requested imageRef.
func (c *Client) DeleteImage(ctx context.Context, imageRef, arch string) error {
	if imageRef == "" || arch == "" {
		return errors.New("imageRef and arch are required")
	}

	path := fmt.Sprintf("/v1/images/%s?arch=%s", imageRef, arch)
	_, err := c.doDeleteRequest(ctx, path)
	return err
}
