// Copyright (c) 2018-2020, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	jsonresp "github.com/sylabs/json-resp"
)

// EntityResponse - Response from the API for an Entity request
type EntityResponse struct {
	Data  Entity          `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// CollectionResponse - Response from the API for an Collection request
type CollectionResponse struct {
	Data  Collection      `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// ContainerResponse - Response from the API for an Container request
type ContainerResponse struct {
	Data  Container       `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// ImageResponse - Response from the API for an Image request
type ImageResponse struct {
	Data  Image           `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// TagsResponse - Response from the API for a tags request
type TagsResponse struct {
	Data  TagMap          `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// ArchTagsResponse - Response from the API for a v2 tags request (with arch)
type ArchTagsResponse struct {
	Data  ArchTagMap      `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// SearchResults - Results structure for searches
type SearchResults struct {
	Entities    []Entity     `json:"entity"`
	Collections []Collection `json:"collection"`
	Containers  []Container  `json:"container"`
	Images      []Image      `json:"image"`
}

// SearchResponse - Response from the API for a search request
type SearchResponse struct {
	Data  SearchResults   `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// UploadImage - Contains requisite data for direct S3 image upload support
type UploadImage struct {
	UploadURL string `json:"uploadURL"`
}

// UploadImageResponse - Response from the API for an image upload request
type UploadImageResponse struct {
	Data  UploadImage     `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// QuotaResponse contains quota usage and total available storage
type QuotaResponse struct {
	QuotaTotalBytes int64 `json:"quotaTotal"`
	QuotaUsageBytes int64 `json:"quotaUsage"`
}

// UploadImageComplete contains data from upload image completion
type UploadImageComplete struct {
	Quota        QuotaResponse `json:"quota"`
	ContainerURL string        `json:"containerUrl"`
}

// UploadImageCompleteResponse is the response to the upload image completion request
type UploadImageCompleteResponse struct {
	Data  UploadImageComplete `json:"data"`
	Error *jsonresp.Error     `json:"error,omitempty"`
}

// MultipartUpload - Contains data for multipart image upload start request
type MultipartUpload struct {
	UploadID   string            `json:"uploadID"`
	TotalParts int               `json:"totalParts"`
	PartSize   int64             `json:"partSize"`
	Options    map[string]string `json:"options"`
}

// MultipartUploadStartResponse - Response from the API for a multipart image upload start request
type MultipartUploadStartResponse struct {
	Data  MultipartUpload `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// UploadImagePart - Contains data for multipart image upload part request
type UploadImagePart struct {
	PresignedURL string `json:"presignedURL"`
}

// UploadImagePartResponse - Response from the API for a multipart image upload part request
type UploadImagePartResponse struct {
	Data  UploadImagePart `json:"data"`
	Error *jsonresp.Error `json:"error,omitempty"`
}

// CompleteMultipartUploadResponse - Response from the API for a multipart image upload complete request
type CompleteMultipartUploadResponse struct {
	Data  UploadImageComplete `json:"data"`
	Error *jsonresp.Error     `json:"error,omitempty"`
}
