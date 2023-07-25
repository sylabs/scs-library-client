package client

import (
	"context"
	"net/url"
)

// DeleteImage deletes requested imageRef.
func (c *Client) DeleteImage(ctx context.Context, imageRef, arch string) error {
	if imageRef == "" || arch == "" {
		return errImageRefArchRequired
	}

	_, err := c.doDeleteRequest(ctx, "v1/images/"+imageRef+"?arch="+url.QueryEscape(arch))
	return err
}
