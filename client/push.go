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
	"time"

	"github.com/golang/glog"
	jsonresp "github.com/sylabs/json-resp"
)

// Timeout for the main upload (not api calls)
const pushTimeout = time.Duration(1800 * time.Second)

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

// UpdateImageSpec defines the parameters of the image being uploaded.
// Size and Hash are image size and the calculated hash (using ImageHash),
// respectively.
type UpdateImageSpec struct {
	SrcReader io.Reader
	Size      int64
	Hash      string
}

// UploadImage will push a specified image up to the Container Library,
func (c *Client) UploadImage(uploadSpec UpdateImageSpec, libraryRef, description string, callback UploadCallback) error {

	if !IsLibraryPushRef(libraryRef) {
		return fmt.Errorf("Not a valid library reference: %s", libraryRef)
	}

	entityName, collectionName, containerName, tags := ParseLibraryRef(libraryRef)

	// Find or create entity
	entity, found, err := c.getEntity(entityName)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Entity %s does not exist in library - creating it.", entityName)
		entity, err = c.createEntity(entityName)
		if err != nil {
			return err
		}
	}

	// Find or create collection
	collection, found, err := c.getCollection(entityName + "/" + collectionName)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Collection %s does not exist in library - creating it.", collectionName)
		collection, err = c.createCollection(collectionName, entity.ID)
		if err != nil {
			return err
		}
	}

	// Find or create container
	container, found, err := c.getContainer(entityName + "/" + collectionName + "/" + containerName)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Container %s does not exist in library - creating it.", containerName)
		container, err = c.createContainer(containerName, collection.ID)
		if err != nil {
			return err
		}
	}

	// Find or create image
	image, found, err := c.GetImage(entityName + "/" + collectionName + "/" + containerName + ":" + uploadSpec.Hash)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Image %s does not exist in library - creating it.", uploadSpec.Hash)
		image, err = c.createImage(uploadSpec.Hash, container.ID, description)
		if err != nil {
			return err
		}
	}

	if !image.Uploaded {
		glog.Info("Now uploading to the library")
		err = c.postFile(uploadSpec, image.ID, callback)
		if err != nil {
			return err
		}
		glog.V(2).Infof("Upload completed OK")
	} else {
		glog.Infof("Image is already present in the library - not uploading.")
	}

	glog.V(2).Infof("Setting tags against uploaded image")
	err = c.setTags(container.ID, image.ID, tags)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) postFile(uploadSpec UpdateImageSpec, imageID string, callback UploadCallback) error {

	postURL := "/v1/imagefile/" + imageID
	glog.V(2).Infof("postFile calling %s", postURL)

	var bodyProgress io.Reader

	if callback != nil {
		// use callback to set up source file reader
		callback.InitUpload(uploadSpec.Size, uploadSpec.SrcReader)
		defer callback.Finish()

		bodyProgress = callback.GetReader()
	} else {
		bodyProgress = uploadSpec.SrcReader
	}

	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()

	// Make an upload request
	req, _ := c.newRequest("POST", postURL, "", bodyProgress)
	// Content length is required by the API
	req.ContentLength = uploadSpec.Size
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
