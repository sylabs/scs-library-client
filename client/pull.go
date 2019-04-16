// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang/glog"
	jsonresp "github.com/sylabs/json-resp"
)

// Timeout for an image pull - could be a large download...
const pullTimeout = time.Duration(1800 * time.Second)

// DownloadImage will retrieve an image from the Container Library,
// saving it into the specified file
func (c *Client) DownloadImage(w io.Writer, path, tag string, callback func(int64, io.Reader, io.Writer) error) error {

	if strings.Contains(path, ":") {
		return fmt.Errorf("Malformed image path: %s", path)
	}

	if tag == "" {
		tag = "latest"
	}

	url := fmt.Sprintf("/v1/imagefile/%s:%s", path, tag)

	glog.V(2).Infof("Pulling from URL: %s", url)

	req, err := c.newRequest(http.MethodGet, url, "", nil)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), pullTimeout)
	defer cancel()

	res, err := c.HTTPClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("The requested image was not found in the library")
	}

	if res.StatusCode != http.StatusOK {
		err := jsonresp.ReadError(res.Body)
		if err != nil {
			return fmt.Errorf("Download did not succeed: %v", err)
		}
		return fmt.Errorf("unexpected http status code: %d", res.StatusCode)
	}

	glog.V(2).Infof("OK response received, beginning body download")

	if callback != nil {
		err = callback(res.ContentLength, res.Body, w)
	} else {
		_, err = io.Copy(w, res.Body)
	}
	if err != nil {
		return err
	}

	glog.V(2).Infof("Download complete")

	return nil

}
