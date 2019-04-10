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
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
)

// Timeout for an image pull - could be a large download...
const pullTimeout = time.Duration(1800 * time.Second)

// DownloadImage will retrieve an image from the Container Library,
// saving it into the specified file
func DownloadImage(c *Client, filePath, libraryRef string, force bool, callback func(int64, io.Reader, io.Writer) error) error {

	if !IsLibraryPullRef(libraryRef) {
		return fmt.Errorf("Not a valid library reference: %s", libraryRef)
	}

	if filePath == "" {
		_, _, container, tags := parseLibraryRef(libraryRef)
		filePath = fmt.Sprintf("%s_%s.sif", container, tags[0])
		glog.Infof("Download filename not provided. Downloading to: %s", filePath)
	}

	libraryRef = strings.TrimPrefix(libraryRef, "library://")

	if !strings.Contains(libraryRef, ":") {
		libraryRef += ":latest"
	}

	url := "/v1/imagefile/" + libraryRef

	glog.V(2).Infof("Pulling from URL: %s", url)

	if !force {
		if _, err := os.Stat(filePath); err == nil {
			return fmt.Errorf("image file already exists - will not overwrite")
		}
	}

	req, err := c.NewRequest(http.MethodGet, url, "", nil)
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
		jRes, err := ParseErrorBody(res.Body)
		if err != nil {
			jRes = ParseErrorResponse(res)
		}
		return fmt.Errorf("Download did not succeed: %d %s\n\t%v",
			jRes.Error.Code, jRes.Error.Status, jRes.Error.Message)
	}

	glog.V(2).Infof("OK response received, beginning body download")

	// Perms are 777 *prior* to umask
	out, err := os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0777)
	if err != nil {
		return err
	}
	defer out.Close()

	glog.V(2).Infof("Created output file: %s", filePath)

	if callback != nil {
		err = callback(res.ContentLength, res.Body, out)
	} else {
		_, err = io.Copy(out, res.Body)
	}
	if err != nil {
		return err
	}

	glog.V(2).Infof("Download complete")

	return nil

}
