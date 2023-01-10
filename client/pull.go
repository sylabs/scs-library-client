// Copyright (c) 2018-2021, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	jsonresp "github.com/sylabs/json-resp"
	"golang.org/x/sync/errgroup"
)

var (
	errUnauthorized          = errors.New("unauthorized")
	errMissingLocationHeader = errors.New("missing HTTP Location header")
	errInvalidArguments      = errors.New("invalid argument(s)")
	errUnknownContentLength  = errors.New("unknown content length")
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

	req, err := c.newRequest(ctx, http.MethodGet, apiPath, q.Encode(), nil)
	if err != nil {
		return err
	}

	res, err := c.HTTPClient.Do(req)
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

// partSpec defines one part of multi-part (concurrent) download.
type partSpec struct {
	Start      int64
	End        int64
	BufferSize int64
}

// Downloader defines concurrency (# of requests) and part size for download operation.
type Downloader struct {
	// Concurrency defines concurrency for multi-part downloads.
	Concurrency uint

	// PartSize specifies size of part for multi-part downloads. Default is 5 MiB.
	PartSize int64

	// BufferSize specifies buffer size used for multi-part downloader routine.
	// Default is 32 KiB.
	BufferSize int64
}

// httpGetRangeRequest performs HTTP GET range request to URL 'u' in range start-end.
func (c *Client) httpGetRangeRequest(ctx context.Context, endpoint, authToken string, start, end int64) (*http.Response, error) {
	return c.httpRangeRequest(ctx, http.MethodGet, endpoint, authToken, start, end)
}

func (c *Client) httpRangeRequest(ctx context.Context, method, endpoint, authToken string, start, end int64) (*http.Response, error) {
	if start >= end || start < 0 || end < 0 || (end-start+1) < 0 {
		return nil, errInvalidArguments
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}

	if v := c.UserAgent; v != "" {
		req.Header.Set("User-Agent", v)
	}

	if authToken != "" && samehost(c.BaseURL, u) {
		// Include authorization header if request being made to host specified by base URL
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %v", authToken))
	}

	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	return c.HTTPClient.Do(req)
}

// samehost returns true if host1 and host2 are, in fact, the same host by
// comparing scheme (https == https) and host (which includes port).
//
// Hosts will be treated as dissimilar if one host includes domain suffix
// and the other does not, even if the host names match.
func samehost(host1, host2 *url.URL) bool {
	return host1.Scheme == host2.Scheme && host1.Host == host2.Host
}

// downloadFilePart writes range to dst as specified in bufferSpec.
func (c *Client) downloadFilePart(ctx context.Context, dst *os.File, endpoint, authToken string, ps *partSpec, pb ProgressBar) error {
	resp, err := c.httpGetRangeRequest(ctx, endpoint, authToken, ps.Start, ps.End)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// allocate transfer buffer for part
	buf := make([]byte, ps.BufferSize)

	for bytesRead := int64(0); bytesRead < ps.End-ps.Start+1; {
		n, err := io.ReadFull(resp.Body, buf)

		// EOF and unexpected EOF shouldn't be handled as errors since short
		// reads are expected if the part size is less than buffer size e.g.
		// the last part if part isn't on size boundary.
		if err != nil && n == 0 {
			return err
		}

		pb.IncrBy(n)

		// WriteAt() is a wrapper around pwrite() which is an atomic
		// seek-and-write operation.
		if _, err := dst.WriteAt(buf[:n], ps.Start+bytesRead); err != nil {
			return err
		}
		bytesRead += int64(n)
	}
	return nil
}

// downloadWorker is a worker func for processing jobs in stripes channel.
func (c *Client) downloadWorker(ctx context.Context, dst *os.File, endpoint, authToken string, parts <-chan partSpec, pb ProgressBar) func() error {
	return func() error {
		for ps := range parts {
			if err := c.downloadFilePart(ctx, dst, endpoint, authToken, &ps, pb); err != nil {
				return err
			}
		}
		return nil
	}
}

// parseContentRangeHeader returns size returned in Content-Range response HTTP header
func parseContentRangeHeader(value string) (int64, error) {
	if value == "" {
		return -1, nil
	}

	vals := strings.Split(value, "/")
	if len(vals) < 2 {
		return 0, errUnknownContentLength
	}
	if vals[1] == "*" {
		// Server reports size is unknown
		return 0, fmt.Errorf("indeterminant size")
	}
	return strconv.ParseInt(vals[1], 0, 64)
}

func (c *Client) getContentLength(ctx context.Context, endpoint, authToken string) (int64, error) {
	// Perform short request to determine content length.
	resp, err := c.httpRangeRequest(ctx, http.MethodHead, endpoint, authToken, 0, 1024)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusNotFound {
			return 0, fmt.Errorf("requested image was not found in the library")
		}
		return 0, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}

	// Extract size from Content-Range header
	return parseContentRangeHeader(resp.Header.Get("Content-Range"))
}

// NoopProgressBar implements ProgressBarInterface to allow disabling the progress bar
type NoopProgressBar struct{}

// Init is a no-op
func (*NoopProgressBar) Init(int64) {}

// ProxyReader is a no-op
func (*NoopProgressBar) ProxyReader(r io.Reader) io.ReadCloser { return io.NopCloser(r) }

// IncrBy is a no-op
func (*NoopProgressBar) IncrBy(int) {}

// Abort is a no-op
func (*NoopProgressBar) Abort(bool) {}

// Wait is a no-op
func (*NoopProgressBar) Wait() {}

// ProgressBar provides a minimal interface for interacting with a progress bar.
// Init is called prior to concurrent download operation.
type ProgressBar interface {
	// Initialize progress bar. Argument is size of file to set progress bar limit.
	Init(int64)

	// ProxyReader wraps r with metrics required for progress tracking. Only useful for
	// single stream downloads.
	ProxyReader(io.Reader) io.ReadCloser

	// IncrBy increments the progress bar. It is called after each concurrent
	// buffer transfer.
	IncrBy(int)

	// Abort terminates the progress bar.
	Abort(bool)

	// Wait waits for the progress bar to complete.
	Wait()
}

// ConcurrentDownloadImage implements a multi-part (concurrent) downloader for
// Cloud Library images. spec is used to define transfer parameters. pb is an
// optional progress bar interface.  If pb is nil, NoopProgressBar is used.
//
// The downloader will handle source files of all sizes and is not limited to
// only files larger than Downloader.PartSize. It will automatically adjust the
// concurrency for source files that do not meet minimum size for multi-part
// downloads.
func (c *Client) ConcurrentDownloadImage(ctx context.Context, dst *os.File, arch, path, tag string, spec *Downloader, pb ProgressBar) error {
	if pb == nil {
		pb = &NoopProgressBar{}
	}

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
		Transport: c.HTTPClient.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.Response.StatusCode == http.StatusSeeOther {
				return http.ErrUseLastResponse
			}
			maxRedir := 10
			if len(via) >= maxRedir {
				return fmt.Errorf("stopped after %d redirects", maxRedir)
			}
			return nil
		},
		Jar:     c.HTTPClient.Jar,
		Timeout: c.HTTPClient.Timeout,
	}

	req, err := c.newRequest(ctx, http.MethodGet, apiPath, q.Encode(), nil)
	if err != nil {
		return err
	}

	res, err := customHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("requested image was not found in the library")
	}

	if res.StatusCode == http.StatusOK {
		// Library endpoint does not provide HTTP redirection response, treat as single stream, direct download
		c.Logger.Logf("Library endpoint does not support concurrent downloads; reverting to single stream")

		return c.singleStreamDownload(ctx, dst, res, pb)
	}

	if res.StatusCode != http.StatusSeeOther {
		if res.StatusCode == http.StatusUnauthorized {
			return errUnauthorized
		}

		return fmt.Errorf("unexpected HTTP status %d: %v", res.StatusCode, err)
	}

	location := res.Header.Get("Location")
	if location == "" {
		return errMissingLocationHeader
	}

	u, err := url.Parse(location)
	if err != nil {
		return fmt.Errorf("parsing redirect URL %v: %v", location, err)
	}

	authToken := ""
	if samehost(c.BaseURL, u) {
		authToken = c.AuthToken
	}

	contentLength, err := c.getContentLength(ctx, u.String(), authToken)
	if err != nil {
		return err
	}

	numParts := uint(1 + (contentLength-1)/spec.PartSize)

	c.Logger.Logf("size: %d, parts: %d, concurrency: %d, partsize: %d, bufsize: %d",
		contentLength, numParts, spec.Concurrency, spec.PartSize, spec.BufferSize,
	)

	jobs := make(chan partSpec)

	g, ctx := errgroup.WithContext(ctx)

	// initialize progress bar
	pb.Init(contentLength)
	defer pb.Wait()

	// if spec.Requests is greater than number of parts for requested file,
	// set concurrency to number of parts
	concurrency := spec.Concurrency
	if numParts < spec.Concurrency {
		concurrency = numParts
	}

	// start workers to manage concurrent HTTP requests
	for workerID := uint(0); workerID <= concurrency; workerID++ {
		g.Go(c.downloadWorker(ctx, dst, u.String(), authToken, jobs, pb))
	}

	// iterate over parts, adding to job queue
	for part := uint(0); part < numParts; part++ {
		partSize := spec.PartSize
		if part == numParts-1 {
			partSize = contentLength - int64(numParts-1)*spec.PartSize
		}

		ps := partSpec{
			Start:      int64(part) * spec.PartSize,
			End:        int64(part)*spec.PartSize + partSize - 1,
			BufferSize: spec.BufferSize,
		}

		jobs <- ps
	}

	close(jobs)

	// wait on errgroup
	if err := g.Wait(); err != nil {
		// cancel/remove progress bar on error
		pb.Abort(true)
		return err
	}
	return nil
}

func (c *Client) singleStreamDownload(ctx context.Context, fp *os.File, res *http.Response, pb ProgressBar) error {
	contentLength := int64(-1)
	val := res.Header.Get("Content-Length")
	if val != "" {
		var err error
		if contentLength, err = strconv.ParseInt(val, 0, 64); err != nil {
			return err
		}
	}
	pb.Init(contentLength)

	proxyReader := pb.ProxyReader(res.Body)
	defer proxyReader.Close()

	if _, err := io.Copy(fp, proxyReader); err != nil {
		return err
	}
	return nil
}
