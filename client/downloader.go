// Copyright (c) 2021-2023, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
)

// filePartDescriptor defines one part of multipart download.
type filePartDescriptor struct {
	start int64
	end   int64
	cur   int64

	w io.WriterAt
}

// Write writes buffer 'p' at offset 'start' using 'WriteAt()' to atomically seek and write.
// Returns bytes written
func (ps *filePartDescriptor) Write(p []byte) (n int, err error) {
	n, err = ps.w.WriteAt(p, ps.start+ps.cur)
	ps.cur += int64(n)

	return
}

// minInt64 returns minimum value of two arguments
func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// Download performs download of contents at url by writing 'size' bytes to 'dst' using credentials 'c'.
func (c *Client) multipartDownload(ctx context.Context, u string, creds credentials, w io.WriterAt, size int64, spec *Downloader, pb ProgressBar) error {
	if size <= 0 {
		return fmt.Errorf("%w: invalid image size (%v)", errBadRequest, size)
	}

	// Initialize the progress bar using passed size
	pb.Init(size)

	// Clean up (remove) progress bar after download
	defer pb.Wait()

	// Calculate # of parts
	parts := uint(1 + (size-1)/spec.PartSize)

	c.Logger.Logf("size: %d, parts: %d, streams: %d, partsize: %d", size, parts, spec.Concurrency, spec.PartSize)

	g, ctx := errgroup.WithContext(ctx)

	// Allocate channel for file part requests
	ch := make(chan filePartDescriptor, parts)

	// Create download part workers
	for n := uint(0); n < spec.Concurrency; n++ {
		g.Go(c.ociDownloadWorker(ctx, u, creds, ch, pb))
	}

	// Add part download requests
	for n := uint(0); n < parts; n++ {
		partSize := minInt64(spec.PartSize, size-int64(n)*spec.PartSize)

		ch <- filePartDescriptor{start: int64(n) * spec.PartSize, end: int64(n)*spec.PartSize + partSize - 1, w: w}
	}

	// Close worker queue after submitting all requests
	close(ch)

	// Wait for workers to complete
	return g.Wait()
}

func (c *Client) ociDownloadWorker(ctx context.Context, u string, creds credentials, ch chan filePartDescriptor, pb ProgressBar) func() error {
	return func() error {
		// Iterate on channel 'ch' to handle download part requests
		for ps := range ch {
			written, err := c.ociDownloadBlobPart(ctx, creds, u, &ps)
			if err != nil {
				// Cleanly abort progress bar on error
				pb.Abort(true)

				return err
			}

			// Increase progress bar by number of bytes downloaded/written
			pb.IncrBy(int(written))
		}
		return nil
	}
}

func (c *Client) ociDownloadBlobPart(ctx context.Context, creds credentials, u string, ps *filePartDescriptor) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}

	if creds != nil {
		if err := creds.ModifyRequest(req); err != nil {
			return 0, err
		}
	}

	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", ps.start, ps.end))

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	return io.Copy(ps, res.Body)
}

// parseContentRange parses "Content-Range" header (eg. "Content-Range: bytes 0-1000/2000") and returns size
func parseContentRange(val string) (int64, error) {
	e := strings.Split(val, " ")

	if !strings.EqualFold(e[0], "bytes") {
		return 0, errUnexpectedMalformedValue
	}

	rangeElems := strings.Split(e[1], "/")

	if len(rangeElems) != 2 {
		return 0, errUnexpectedMalformedValue
	}

	return strconv.ParseInt(rangeElems[1], 10, 0)
}
