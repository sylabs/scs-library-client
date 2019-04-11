// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
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

// UploadImage will push a specified image up to the Container Library,
func UploadImage(c *Client, filePath, libraryRef, description string, callback UploadCallback) error {

	if !IsLibraryPushRef(libraryRef) {
		return fmt.Errorf("Not a valid library reference: %s", libraryRef)
	}

	imageHash, err := ImageHash(filePath)
	if err != nil {
		return err
	}
	glog.V(2).Infof("Image hash computed as %s", imageHash)

	entityName, collectionName, containerName, tags := parseLibraryRef(libraryRef)

	// Find or create entity
	entity, found, err := GetEntity(c, entityName)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Entity %s does not exist in library - creating it.", entityName)
		entity, err = CreateEntity(c, entityName)
		if err != nil {
			return err
		}
	}

	// Find or create collection
	collection, found, err := GetCollection(c, entityName+"/"+collectionName)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Collection %s does not exist in library - creating it.", collectionName)
		collection, err = CreateCollection(c, collectionName, entity.GetID().Hex())
		if err != nil {
			return err
		}
	}

	// Find or create container
	container, found, err := getContainer(c, entityName+"/"+collectionName+"/"+containerName)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Container %s does not exist in library - creating it.", containerName)
		container, err = createContainer(c, containerName, collection.GetID().Hex())
		if err != nil {
			return err
		}
	}

	// Find or create image
	image, found, err := getImage(c, entityName+"/"+collectionName+"/"+containerName+":"+imageHash)
	if err != nil {
		return err
	}
	if !found {
		glog.V(1).Infof("Image %s does not exist in library - creating it.", imageHash)
		image, err = createImage(c, imageHash, container.GetID().Hex(), description)
		if err != nil {
			return err
		}
	}

	if !image.Uploaded {
		glog.Infof("Now uploading %s to the library", filePath)
		err = postFile(c, filePath, image.GetID().Hex(), callback)
		if err != nil {
			return err
		}
		glog.V(2).Infof("Upload completed OK")
	} else {
		glog.Infof("Image is already present in the library - not uploading.")
	}

	glog.V(2).Infof("Setting tags against uploaded image")
	err = SetTags(c, container.GetID().Hex(), image.GetID().Hex(), tags)
	if err != nil {
		return err
	}

	return nil
}

func postFile(c *Client, filePath, imageID string, callback UploadCallback) error {

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("Could not open the image file to upload: %v", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("Could not find size of the image file to upload: %v", err)
	}
	fileSize := fi.Size()

	postURL := "/v1/imagefile/" + imageID
	glog.V(2).Infof("postFile calling %s", postURL)

	b := bufio.NewReader(f)

	var bodyProgress io.Reader

	if callback != nil {
		// use callback to set up source file reader
		callback.InitUpload(fileSize, b)
		defer callback.Finish()

		bodyProgress = callback.GetReader()
	} else {
		bodyProgress = b
	}

	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()

	// Make an upload request
	req, _ := c.NewRequest("POST", postURL, "", bodyProgress)
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
