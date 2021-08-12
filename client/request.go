// Copyright (c) 2019-2020, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

// UploadImageRequest is sent to initiate V2 image upload
type UploadImageRequest struct {
	Size           int64  `json:"filesize"`
	MD5Checksum    string `json:"md5sum,omitempty"`
	SHA256Checksum string `json:"sha256sum,omitempty"`
}

// UploadImageCompleteRequest is sent to complete V2 image upload; it is
// currently unused.
type UploadImageCompleteRequest struct{}

// MultipartUploadStartRequest is sent to initiate V2 multipart image upload
type MultipartUploadStartRequest struct {
	Size int64 `json:"filesize"`
}

// UploadImagePartRequest is sent prior to each part in a multipart upload
type UploadImagePartRequest struct {
	PartSize       int64  `json:"partSize"`
	UploadID       string `json:"uploadID"`
	PartNumber     int    `json:"partNumber"`
	SHA256Checksum string `json:"sha256sum"`
}

// CompletedPart represents a single part of a multipart image upload
type CompletedPart struct {
	PartNumber int    `json:"partNumber"`
	Token      string `json:"token"`
}

// CompleteMultipartUploadRequest is sent to complete V2 multipart image upload
type CompleteMultipartUploadRequest struct {
	UploadID       string          `json:"uploadID"`
	CompletedParts []CompletedPart `json:"completedParts"`
}

// AbortMultipartUploadRequest is sent to abort V2 multipart image upload
type AbortMultipartUploadRequest struct {
	UploadID string `json:"uploadID"`
}
