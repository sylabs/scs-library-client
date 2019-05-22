// Copyright (c) 2019, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

type UploadImageRequest struct {
	Size int64 `json:"filesize"`
}

type UploadImageCompleteRequest struct {
}
