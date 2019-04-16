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

	"github.com/golang/glog"
	jsonresp "github.com/sylabs/json-resp"
)

// UploadCallback defines an interface used to perform a call-out to
// set up the source file Reader.
type UploadCallback interface {
	// Initializes the callback given a file size and source file Reader
	InitUpload(int64, io.Reader)
	// (optionally) can return a proxied Reader
	GetReader() io.Reader
	// called when the upload operation is complete
	Finish()
}

// UploadImage will push a specified image from an io.ReadSeeker up to the
// Container Library, The timeout value for this operation is set within
// the context. It is recommended to use a large value (ie. 1800 seconds) to
// prevent timeout when uploading large images.
func (c *Client) UploadImage(ctx context.Context, r io.ReadSeeker, path string, tags []string, description string, callback UploadCallback) error {

	entityName, collectionName, containerName, parsedTags := ParseLibraryPath(path)
	if len(parsedTags) != 0 {
		return fmt.Errorf("Malformed image path: %s", path)
	}

	imageHash, fileSize, err := sha256sum(r)
	if err != nil {
		return fmt.Errorf("Error calculating SHA checksum: %v", err)
	}

	// rollback to top of file
	_, err = r.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("Error seeking to start stream: %v", err)
	}

	glog.V(2).Infof("Image hash computed as %s", imageHash)

	// Find or create entity
	entity, found, err := c.getEntity(ctx, entityName)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Entity %s does not exist in library - creating it.", entityName)
		entity, err = c.createEntity(ctx, entityName)
		if err != nil {
			return err
		}
	}

	// Find or create collection
	collection, found, err := c.getCollection(ctx, entityName+"/"+collectionName)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Collection %s does not exist in library - creating it.", collectionName)
		collection, err = c.createCollection(ctx, collectionName, entity.ID)
		if err != nil {
			return err
		}
	}

	// Find or create container
	container, found, err := c.getContainer(ctx, entityName+"/"+collectionName+"/"+containerName)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Container %s does not exist in library - creating it.", containerName)
		container, err = c.createContainer(ctx, containerName, collection.ID)
		if err != nil {
			return err
		}
	}

	// Find or create image
	image, found, err := c.GetImage(ctx, entityName+"/"+collectionName+"/"+containerName+":"+imageHash)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Image %s does not exist in library - creating it.", imageHash)
		image, err = c.createImage(ctx, imageHash, container.ID, description)
		if err != nil {
			return err
		}
	}

	if !image.Uploaded {
		glog.Info("Now uploading to the library")
		err = c.postFile(ctx, r, fileSize, image.ID, callback)
		if err != nil {
			return err
		}
		glog.V(2).Infof("Upload completed OK")
	} else {
		glog.Infof("Image is already present in the library - not uploading.")
	}

	glog.V(2).Infof("Setting tags against uploaded image")
	err = c.setTags(ctx, container.ID, image.ID, append(tags, parsedTags...))
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) postFile(ctx context.Context, r io.ReadSeeker, fileSize int64, imageID string, callback UploadCallback) error {

	postURL := "/v1/imagefile/" + imageID
	glog.V(2).Infof("postFile calling %s", postURL)

	var bodyProgress io.Reader

	if callback != nil {
		// use callback to set up source file reader
		callback.InitUpload(fileSize, r)
		defer callback.Finish()

		bodyProgress = callback.GetReader()
	} else {
		bodyProgress = r
	}

	// Make an upload request
	req, _ := c.newRequest("POST", postURL, "", bodyProgress)
	// Content length is required by the API
	req.ContentLength = fileSize
	res, err := c.HTTPClient.Do(req.WithContext(ctx))

	if err != nil {
		return fmt.Errorf("Error uploading file to server: %s", err.Error())
	}
	if res.StatusCode != http.StatusOK {
		err := jsonresp.ReadError(res.Body)
		if err != nil {
			return fmt.Errorf("Sending file did not succeed: %v", err)
		}
		return fmt.Errorf("Sending file did not succeed: http status code %d", res.StatusCode)
	}

	return nil
}
