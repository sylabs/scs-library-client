// Copyright (c) 2018-2020, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	jsonresp "github.com/sylabs/json-resp"
	"golang.org/x/sync/errgroup"
)

const (
	// minimumPartSize is the minimum size of a part in a multipart upload;
	// this liberty is taken by defining this value on the client-side to
	// prevent a round-trip to the server. The server will return HTTP status
	// 400 if the requested multipart upload size is less than 5MiB.
	minimumPartSize = 64 * 1024 * 1024

	// OptionS3Compliant indicates a 100% S3 compatible object store is being used by backend library server
	OptionS3Compliant = "s3compliant"
)

// UploadCallback defines an interface used to perform a call-out to
// set up the source file Reader.
type UploadCallback interface {
	// Initializes the callback given a file size and source file Reader
	InitUpload(int64, io.Reader)
	// (optionally) can return a proxied Reader
	GetReader() io.Reader
	// TerminateUpload is called if the upload operation is interrupted before completion
	Terminate()
	// called when the upload operation is complete
	Finish()
}

// Default upload callback
type defaultUploadCallback struct {
	r io.Reader
}

func (c *defaultUploadCallback) InitUpload(s int64, r io.Reader) {
	c.r = r
}

func (c *defaultUploadCallback) GetReader() io.Reader {
	return c.r
}

func (c *defaultUploadCallback) Terminate() {
}

func (c *defaultUploadCallback) Finish() {
}

// calculateChecksums uses a TeeReader to calculate MD5 and SHA256
// checksums concurrently
func calculateChecksums(r io.Reader) (string, string, int64, error) {
	pr, pw := io.Pipe()
	tr := io.TeeReader(r, pw)

	var g errgroup.Group

	var md5checksum string
	var sha256checksum string
	var fileSize int64

	// compute MD5 checksum for comparison with S3 checksum
	g.Go(func() error {
		// The pipe writer must be closed so sha256 computation gets EOF and will
		// complete.
		defer pw.Close()
		var err error

		md5checksum, fileSize, err = md5sum(tr)
		if err != nil {
			return fmt.Errorf("error calculating MD5 checksum: %v", err)
		}
		return nil
	})

	// Compute sha256
	g.Go(func() error {
		var err error
		sha256checksum, _, err = sha256sum(pr)
		if err != nil {
			return fmt.Errorf("error calculating SHA checksum: %v", err)
		}
		return nil
	})

	err := g.Wait()
	return md5checksum, sha256checksum, fileSize, err
}

// UploadImage will push a specified image from an io.ReadSeeker up to the
// Container Library, The timeout value for this operation is set within
// the context. It is recommended to use a large value (ie. 1800 seconds) to
// prevent timeout when uploading large images.
func (c *Client) UploadImage(ctx context.Context, r io.ReadSeeker, path, arch string, tags []string, description string, callback UploadCallback) (*UploadImageComplete, error) {
	if !IsLibraryPushRef(path) {
		return nil, fmt.Errorf("malformed image path: %s", path)
	}

	entityName, collectionName, containerName, parsedTags := ParseLibraryPath(path)
	if len(parsedTags) != 0 {
		return nil, fmt.Errorf("malformed image path: %s", path)
	}

	// calculate sha256 and md5 checksums
	md5Checksum, imageHash, fileSize, err := calculateChecksums(r)
	if err != nil {
		return nil, fmt.Errorf("error calculating checksums: %v", err)
	}

	// rollback to top of file
	if _, err = r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("error seeking to start stream: %v", err)
	}

	c.Logger.Logf("Image hash computed as %s", imageHash)

	// Find or create entity
	entity, err := c.getEntity(ctx, entityName)
	if err != nil {
		if err != ErrNotFound {
			return nil, err
		}
		c.Logger.Logf("Entity %s does not exist in library - creating it.", entityName)
		entity, err = c.createEntity(ctx, entityName)
		if err != nil {
			return nil, err
		}
	}

	// Find or create collection
	qualifiedCollectionName := fmt.Sprintf("%s/%s", entityName, collectionName)
	collection, err := c.getCollection(ctx, qualifiedCollectionName)
	if err != nil {
		if err != ErrNotFound {
			return nil, err
		}
		// create collection
		c.Logger.Logf("Collection %s does not exist in library - creating it.", collectionName)
		collection, err = c.createCollection(ctx, collectionName, entity.ID)
		if err != nil {
			return nil, err
		}
	}

	// Find or create container
	computedName := fmt.Sprintf("%s/%s", qualifiedCollectionName, containerName)
	container, err := c.getContainer(ctx, computedName)
	if err != nil {
		if err != ErrNotFound {
			return nil, err
		}
		// Create container
		c.Logger.Logf("Container %s does not exist in library - creating it.", containerName)
		container, err = c.createContainer(ctx, containerName, collection.ID)
		if err != nil {
			return nil, err
		}
	}

	// Find or create image
	image, err := c.GetImage(ctx, arch, computedName+":"+"sha256."+imageHash)
	if err != nil {
		if err != ErrNotFound {
			return nil, err
		}
		// Create image
		c.Logger.Logf("Image %s does not exist in library - creating it.", imageHash)
		image, err = c.createImage(ctx, "sha256."+imageHash, container.ID, description)
		if err != nil {
			return nil, err
		}
	}

	var res *UploadImageComplete

	if !image.Uploaded {
		// upload image

		if callback == nil {
			// use default (noop) upload callback
			callback = &defaultUploadCallback{r: r}
		}

		metadata := map[string]string{
			"sha256sum": imageHash,
			"md5sum":    md5Checksum,
		}

		res, err = c.postFileWrapper(ctx, r, fileSize, image.ID, callback, metadata)
		if err != nil {
			return nil, err
		}
	} else {
		c.Logger.Logf("Image is already present in the library - not uploading.")
	}

	// set tags on image
	c.Logger.Logf("Setting tags against uploaded image")

	if c.apiAtLeast(ctx, APIVersionV2ArchTags) {
		if err := c.setTagsV2(ctx, container.ID, arch, image.ID, append(tags, parsedTags...)); err != nil {
			return nil, err
		}
		return res, nil
	}

	c.Logger.Logf("This library does not support multiple architectures per tag.")

	c.Logger.Logf("This tag will replace any already uploaded with the same name.")

	if err := c.setTags(ctx, container.ID, image.ID, append(tags, parsedTags...)); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Client) postFileWrapper(ctx context.Context, r io.ReadSeeker, fileSize int64, imageID string, callback UploadCallback, metadata map[string]string) (*UploadImageComplete, error) {
	var err error

	// use callback to set up source file reader
	callback.InitUpload(fileSize, r)

	var res *UploadImageComplete

	c.Logger.Log("Now uploading to the library")

	if c.apiAtLeast(ctx, APIVersionV2Upload) {
		// use v2 post file api. Send both md5 and sha256 checksums. If the
		// remote does not support sha256, it will be ignored and fallback
		// to md5. If the remote is aware of sha256, will be used and md5
		// will be ignored.
		res, err = c.postFileV2(ctx, r, fileSize, imageID, callback, metadata)
	} else {
		// fallback to legacy upload
		res, err = c.postFile(ctx, fileSize, imageID, callback)
	}

	if err != nil {
		callback.Terminate()

		c.Logger.Log("Upload terminated due to error")
	} else {
		callback.Finish()

		c.Logger.Log("Upload completed OK")
	}

	return res, err
}

func (c *Client) postFile(ctx context.Context, fileSize int64, imageID string, callback UploadCallback) (*UploadImageComplete, error) {
	postURL := "v1/imagefile/" + imageID

	c.Logger.Logf("postFile calling %s", postURL)

	// Make an upload request
	req, _ := c.newRequest(http.MethodPost, postURL, "", callback.GetReader())
	// Content length is required by the API
	req.ContentLength = fileSize
	res, err := c.HTTPClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("error uploading file to server: %s", err.Error())
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		if err := jsonresp.ReadError(res.Body); err != nil {
			return nil, fmt.Errorf("sending file did not succeed: %v", err)
		}
		return nil, fmt.Errorf("sending file did not succeed: http status code %d", res.StatusCode)
	}
	return nil, nil
}

// postFileV2 uses V2 API to upload images to SCS library server. This is
// a three step operation: "create" upload image request, which returns a
// URL to issue an http PUT operation against, and then finally calls the
// completion endpoint once upload is complete.
func (c *Client) postFileV2(ctx context.Context, r io.ReadSeeker, fileSize int64, imageID string, callback UploadCallback, metadata map[string]string) (*UploadImageComplete, error) {
	if fileSize > minimumPartSize {
		// only attempt multipart upload if size greater than S3 minimum
		c.Logger.Log("Attempting to use multipart uploader")

		var err error
		var res *UploadImageComplete

		res, err = c.postFileV2Multipart(ctx, r, fileSize, imageID, callback)
		if err != nil {
			// if the error is anything other than ErrNotFound, fallback to legacy (single part)
			// uploader.
			if err != ErrNotFound {
				return nil, err
			}
			// fallthrough to legacy (single part) uploader
		} else {
			// multipart upload successful
			return res, nil
		}
	}

	// fallback to legacy uploader
	c.Logger.Log("Using legacy (single part) uploader")

	return c.legacyPostFileV2(ctx, fileSize, imageID, callback, metadata)
}

// uploadManager contains common params for multipart part function
type uploadManager struct {
	Source   io.ReadSeeker
	Size     int64
	ImageID  string
	UploadID string
}

func (c *Client) postFileV2Multipart(ctx context.Context, r io.ReadSeeker, fileSize int64, imageID string, callback UploadCallback) (*UploadImageComplete, error) {
	// initiate multipart upload with backend to determine number of expected
	// parts and part size
	response, err := c.startMultipartUpload(ctx, fileSize, imageID)
	if err != nil {
		c.Logger.Logf("Error starting multipart upload: %v", err)

		return nil, err
	}

	c.Logger.Logf("Multi-part upload: ID=[%s] totalParts=[%d] partSize=[%d]", response.UploadID, response.TotalParts, fileSize)

	// Enable S3 compliance mode by default
	val := response.Options[OptionS3Compliant]
	s3Compliant := val == "" || val == "true"

	c.Logger.Logf("S3 compliant option: %v", s3Compliant)

	// maintain list of completed parts which will be passed to the completion function
	completedParts := []CompletedPart{}

	bytesRemaining := fileSize

	for nPart := 1; nPart <= response.TotalParts; nPart++ {
		partSize := getPartSize(bytesRemaining, response.PartSize)

		c.Logger.Logf("Uploading part %d (%d bytes)", nPart, partSize)

		mgr := &uploadManager{
			Source:   r,
			Size:     partSize,
			ImageID:  imageID,
			UploadID: response.UploadID,
		}

		// include "X-Amz-Content-Sha256" header only if object store is 100% S3 compatible
		etag, err := c.multipartUploadPart(ctx, nPart, mgr, callback, s3Compliant)
		if err != nil {
			// error uploading part
			c.Logger.Logf("Error uploading part %d: %v", nPart, err)

			if err := c.abortMultipartUpload(ctx, mgr); err != nil {
				c.Logger.Logf("Error aborting multipart upload: %v", err)
			}
			return nil, err
		}

		// append completed part info to list
		completedParts = append(completedParts, CompletedPart{PartNumber: nPart, Token: etag})

		// decrement upload bytes remaining
		bytesRemaining -= partSize
	}

	c.Logger.Logf("Uploaded %d parts", response.TotalParts)

	return c.completeMultipartUpload(ctx, &completedParts, &uploadManager{
		ImageID:  imageID,
		UploadID: response.UploadID,
	})
}

// getPartSize returns number of bytes to read for "next" part. This value will
// never exceed the S3 maximum
func getPartSize(bytesRemaining int64, partSize int64) int64 {
	if bytesRemaining > int64(partSize) {
		return partSize
	}
	return bytesRemaining
}

func (c *Client) startMultipartUpload(ctx context.Context, fileSize int64, imageID string) (MultipartUpload, error) {
	// attempt to initiate multipart upload
	postURL := fmt.Sprintf("v2/imagefile/%s/_multipart", imageID)

	c.Logger.Logf("startMultipartUpload calling %s", postURL)

	body := MultipartUploadStartRequest{
		Size: fileSize,
	}

	objJSON, err := c.apiCreate(ctx, postURL, body)
	if err != nil {
		return MultipartUpload{}, err
	}

	var res MultipartUploadStartResponse
	if err := json.Unmarshal(objJSON, &res); err != nil {
		return MultipartUpload{}, err
	}
	return res.Data, nil
}

// remoteSHA256ChecksumSupport parses the 'X-Amz-SignedHeaders' from the
// presigned PUT URL query string to determine if 'x-amz-content-sha256' is
// present. If 'x-amz-content-sha256' is present, the remote is expecting the
// SHA256 checksum in the headers of the presigned PUT URL request.
func remoteSHA256ChecksumSupport(u *url.URL) bool {
	hdr := u.Query()["X-Amz-SignedHeaders"]
	if len(hdr) < 1 {
		return false
	}

	for _, h := range strings.Split(hdr[0], ";") {
		if h == "x-amz-content-sha256" {
			return true
		}
	}

	return false
}

func (c *Client) legacyPostFileV2(ctx context.Context, fileSize int64, imageID string, callback UploadCallback, metadata map[string]string) (*UploadImageComplete, error) {
	postURL := fmt.Sprintf("v2/imagefile/%s", imageID)

	c.Logger.Logf("legacyPostFileV2 calling %s", postURL)

	// issue upload request (POST) to obtain presigned S3 URL
	body := UploadImageRequest{
		Size:           fileSize,
		SHA256Checksum: metadata["sha256sum"],
		MD5Checksum:    metadata["md5sum"],
	}

	objJSON, err := c.apiCreate(ctx, postURL, body)
	if err != nil {
		return nil, err
	}

	var res UploadImageResponse
	if err := json.Unmarshal(objJSON, &res); err != nil {
		return nil, err
	}

	// upload (PUT) directly to S3 presigned URL provided above
	presignedURL := res.Data.UploadURL
	if presignedURL == "" {
		return nil, fmt.Errorf("error getting presigned URL")
	}

	parsedURL, err := url.Parse(presignedURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing presigned URL")
	}

	// parse presigned URL to determine if we need to send sha256 checksum
	useSHA256Checksum := remoteSHA256ChecksumSupport(parsedURL)

	req, err := http.NewRequest(http.MethodPut, presignedURL, callback.GetReader())
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	req.ContentLength = fileSize
	req.Header.Set("Content-Type", "application/octet-stream")

	if useSHA256Checksum {
		req.Header.Set("x-amz-content-sha256", metadata["sha256sum"])
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	callback.Finish()
	if err != nil {
		return nil, fmt.Errorf("error uploading image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error uploading image: HTTP status %d", resp.StatusCode)
	}

	// send (PUT) image upload completion
	objJSON, err = c.apiUpdate(ctx, postURL+"/_complete", UploadImageCompleteRequest{})
	if err != nil {
		return nil, fmt.Errorf("error sending upload complete request: %v", err)
	}

	if len(objJSON) == 0 {
		// success w/o detailed upload complete response
		return nil, nil
	}

	var uploadResp UploadImageCompleteResponse
	if err := json.Unmarshal(objJSON, &uploadResp); err != nil {
		return nil, fmt.Errorf("error decoding upload response: %v", err)
	}
	return &uploadResp.Data, nil
}

func getPartSHA256Sum(r io.Reader, size int64) (string, error) {
	// calculate sha256sum of part
	tmpChunk := io.LimitReader(r, size)
	chunkHash, _, err := sha256sum(tmpChunk)
	return chunkHash, err
}

func (c *Client) multipartUploadPart(ctx context.Context, partNumber int, m *uploadManager, callback UploadCallback, includeSHA256ChecksumHeader bool) (string, error) {
	var chunkHash string
	var err error

	if includeSHA256ChecksumHeader {
		// calculate sha256sum of part being uploaded
		chunkHash, err = getPartSHA256Sum(m.Source, int64(m.Size))
		if err != nil {
			c.Logger.Logf("Error calculating SHA256 checksum: %v", err)
			return "", err
		}

		// rollback file pointer to beginning of part
		if _, err := m.Source.Seek(-(int64(m.Size)), io.SeekCurrent); err != nil {
			c.Logger.Logf("Error repositioning file pointer: %v", err)
			return "", err
		}
	}

	// send request to cloud-library for presigned PUT url
	uri := fmt.Sprintf("v2/imagefile/%s/_multipart", m.ImageID)

	c.Logger.Logf("multipartUploadPart calling %s", uri)

	objJSON, err := c.apiUpdate(ctx, uri, UploadImagePartRequest{
		PartSize:       m.Size,
		UploadID:       m.UploadID,
		PartNumber:     partNumber,
		SHA256Checksum: chunkHash,
	})
	if err != nil {
		return "", err
	}

	var res UploadImagePartResponse
	if err := json.Unmarshal(objJSON, &res); err != nil {
		return "", err
	}

	// send request to S3
	req, err := http.NewRequest(http.MethodPut, res.Data.PresignedURL, io.LimitReader(callback.GetReader(), m.Size))
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	// add headers to be signed
	req.ContentLength = m.Size
	if includeSHA256ChecksumHeader {
		req.Header.Add("x-amz-content-sha256", chunkHash)
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		c.Logger.Logf("Failure uploading to presigned URL: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	// process response from S3
	if resp.StatusCode != http.StatusOK {
		c.Logger.Logf("Object store returned an error: %d", resp.StatusCode)
		return "", fmt.Errorf("object store returned an error: %d", resp.StatusCode)
	}

	etag := resp.Header.Get("ETag")

	c.Logger.Logf("Part %d accepted (ETag: %s)", partNumber, etag)

	return etag, nil
}

func (c *Client) completeMultipartUpload(ctx context.Context, completedParts *[]CompletedPart, m *uploadManager) (*UploadImageComplete, error) {
	c.Logger.Logf("Completing multipart upload: %s", m.UploadID)

	uri := fmt.Sprintf("v2/imagefile/%s/_multipart_complete", m.ImageID)

	c.Logger.Logf("completeMultipartUpload calling %s", uri)

	body := CompleteMultipartUploadRequest{
		UploadID:       m.UploadID,
		CompletedParts: *completedParts,
	}

	objJSON, err := c.apiUpdate(ctx, uri, body)
	if err != nil {
		c.Logger.Logf("Error completing multipart upload: %v", err)
		return nil, err
	}

	var res CompleteMultipartUploadResponse
	if err := json.Unmarshal(objJSON, &res); err != nil {
		c.Logger.Logf("Error decoding complete multipart upload request: %v", err)
		return nil, err
	}

	if res.Data.ContainerURL == "" {
		// success w/o detailed upload complete response
		return nil, nil
	}

	return &res.Data, nil
}

func (c *Client) abortMultipartUpload(ctx context.Context, m *uploadManager) error {
	c.Logger.Logf("Aborting multipart upload ID: %s", m.UploadID)

	uri := fmt.Sprintf("v2/imagefile/%s/_multipart_abort", m.ImageID)

	c.Logger.Logf("abortMultipartUpload calling %s", uri)

	body := AbortMultipartUploadRequest{
		UploadID: m.UploadID,
	}

	if _, err := c.apiUpdate(ctx, uri, body); err != nil {
		c.Logger.Logf("error aborting multipart upload: %v", err)
		return err
	}
	return nil
}
