package client

import (
	"context"
	"errors"
	"net/url"
)

// DeleteImage deletes requested imageRef.
func (c *Client) DeleteImage(ctx context.Context, imageRef, arch string) error {
	if imageRef == "" || arch == "" {
		return errors.New("imageRef and arch are required")
	}

	_, err := c.doDeleteRequest(ctx, "v1/images/"+imageRef+"?"+url.QueryEscape("arch="+arch))
	return err
}
