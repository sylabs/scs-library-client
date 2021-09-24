// Copyright (c) 2018-2021, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	jsonresp "github.com/sylabs/json-resp"
	"golang.org/x/sync/errgroup"
)

// DownloadImage will retrieve an image from the Container Library, saving it
// into the specified io.Writer. The timeout value for this operation is set
// within the context. It is recommended to use a large value (ie. 1800 seconds)
// to prevent timeout when downloading large images.
func (c *Client) DownloadImage(ctx context.Context, w io.Writer, arch, path, tag string, callback func(int64, io.Reader, io.Writer) error) error {
	if arch != "" && !c.apiAtLeast(ctx, APIVersionV2ArchTags) {
		c.Logger.Logf("This library does not support architecture specific tags")
		c.Logger.Logf("The image returned may not be the requested architecture")
	}

	if strings.Contains(path, ":") {
		return fmt.Errorf("malformed image path: %s", path)
	}

	if tag == "" {
		tag = "latest"
	}

	apiPath := fmt.Sprintf("v1/imagefile/%s:%s", strings.TrimPrefix(path, "/"), tag)
	q := url.Values{}
	q.Add("arch", arch)

	c.Logger.Logf("Pulling from URL: %s", apiPath)

	req, err := c.newRequest(http.MethodGet, apiPath, q.Encode(), nil)
	if err != nil {
		return err
	}

	res, err := c.HTTPClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("requested image was not found in the library")
	}

	if res.StatusCode != http.StatusOK {
		err := jsonresp.ReadError(res.Body)
		if err != nil {
			return fmt.Errorf("download did not succeed: %v", err)
		}
		return fmt.Errorf("unexpected http status code: %d", res.StatusCode)
	}

	c.Logger.Logf("OK response received, beginning body download")

	if callback != nil {
		err = callback(res.ContentLength, res.Body, w)
	} else {
		_, err = io.Copy(w, res.Body)
	}
	if err != nil {
		return err
	}

	c.Logger.Logf("Download complete")

	return nil
}

// bufferSpec defines one chunk of chunked download.
type bufferSpec struct {
	Begin int64
	End   int64
}

// DownloadSpec defines # of requests and chunk size for download operation.
type DownloadSpec struct {
	Requests  int
	ChunkSize int64
}

// httpGetRangeRequest performs HTTP GET range request to URL specified by 'u' in range start-end.
func httpGetRangeRequest(ctx context.Context, url string, start, end int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	return http.DefaultClient.Do(req)
}

const (
	_        = iota
	kilobyte = 1 << (10 * iota)
)

// downloadFileChunk writes range to file fp as specified in bufferSpec.
func downloadFileChunk(ctx context.Context, dst *os.File, url string, rangeStart, rangeEnd int64, bar ProgressBarInterface) error {
	resp, err := httpGetRangeRequest(ctx, url, rangeStart, rangeEnd)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// use 32KiB buffer to read source
	buf := make([]byte, 32*kilobyte)

	for bytesRead := int64(0); bytesRead < rangeEnd-rangeStart+1; {
		n, err := io.ReadFull(resp.Body, buf)

		// EOF and unexpected EOF shouldn't be handled as errors since short
		// reads are expected if the chunk size is less than buffer size.
		if err != nil && n == 0 {
			return err
		}

		bar.IncrBy(n)

		// WriteAt() is a wrapper around pwrite() which is an atomic
		// seek-and-write operation.
		if _, err := dst.WriteAt(buf[:n], rangeStart+bytesRead); err != nil {
			return err
		}
		bytesRead += int64(n)
	}
	return nil
}

// downloadWorker is a worker func for processing jobs in stripes channel.
func downloadWorker(ctx context.Context, fp *os.File, url string, stripes <-chan bufferSpec, bar ProgressBarInterface) func() error {
	return func() error {
		for bs := range stripes {
			if err := downloadFileChunk(ctx, fp, url, bs.Begin, bs.End, bar); err != nil {
				return err
			}
		}
		return nil
	}
}

func getContentLength(ctx context.Context, url string) (int64, error) {
	// get first file chunk to determine total size.
	resp, err := httpGetRangeRequest(ctx, url, 0, 1024)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusNotFound {
			return 0, fmt.Errorf("requested image was not found in the library")
		} else {
			return 0, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
		}
	}

	vals := strings.Split(resp.Header.Get("Content-Range"), "/")
	return strconv.ParseInt(vals[1], 0, 64)
}

type NoopProgressBar struct{}

func (*NoopProgressBar) Init(int64) {}
func (*NoopProgressBar) IncrBy(int) {}
func (*NoopProgressBar) Wait()      {}

type ProgressBarInterface interface {
	Init(int64)
	IncrBy(int)
	Wait()
}

func (c *Client) ConcurrentDownloadImage(ctx context.Context, fp *os.File, arch, path, tag string, spec *DownloadSpec, pb ProgressBarInterface) error {
	if arch != "" && !c.apiAtLeast(ctx, APIVersionV2ArchTags) {
		c.Logger.Logf("This library does not support architecture specific tags")
		c.Logger.Logf("The image returned may not be the requested architecture")
	}

	if strings.Contains(path, ":") {
		return fmt.Errorf("malformed image path: %s", path)
	}

	if tag == "" {
		tag = "latest"
	}

	apiPath := fmt.Sprintf("v1/imagefile/%s:%s", strings.TrimPrefix(path, "/"), tag)
	q := url.Values{}
	q.Add("arch", arch)

	c.Logger.Logf("Pulling from URL: %s", apiPath)

	customHTTPClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}

	req, err := c.newRequest(http.MethodGet, apiPath, q.Encode(), nil)
	if err != nil {
		return err
	}

	res, err := customHTTPClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("requested image was not found in the library")
	}

	if res.StatusCode != http.StatusSeeOther {
		return fmt.Errorf("unexpected HTTP status %d: %v", res.StatusCode, err)
	}

	url := res.Header.Get("Location")

	contentLength, err := getContentLength(ctx, url)
	if err != nil {
		return err
	}

	numParts := int(1 + (contentLength-1)/spec.ChunkSize)

	jobs := make(chan bufferSpec, numParts)

	g, ctx := errgroup.WithContext(ctx)

	if pb == nil {
		pb = &NoopProgressBar{}
	}

	// initialize progress bar
	pb.Init(contentLength)

	// start workers to manage concurrent HTTP requests
	for workerID := 0; workerID <= spec.Requests; workerID++ {
		g.Go(downloadWorker(ctx, fp, url, jobs, pb))
	}

	// iterate over parts, adding to job queue
	for part := 0; part < numParts; part++ {
		chunkSize := spec.ChunkSize
		if part == numParts-1 {
			chunkSize = contentLength - int64(numParts-1)*spec.ChunkSize
		}

		b := bufferSpec{Begin: int64(part) * spec.ChunkSize}
		b.End = b.Begin + chunkSize - 1

		jobs <- b
	}

	close(jobs)

	// wait on errgroup
	err = g.Wait()

	// wait on progress bar
	pb.Wait()

	return err
}
