// Copyright (c) 2018-2023, Sylabs Inc. All rights reserved.
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
	"strconv"
	"strings"
)

var (
	errUnauthorized  = errors.New("unauthorized")
	errImageNotFound = errors.New("requested image was not found in the library")
)

// Downloader defines concurrency (# of requests) and part size for download operation.
type Downloader struct {
	// Concurrency defines concurrency for multi-part downloads.
	Concurrency uint

	// PartSize specifies size of part for multi-part downloads. Default is 5 MiB.
	PartSize int64

	// BufferSize specifies buffer size used for multi-part downloader routine.
	// Default is 32 KiB.
	// Deprecated: this value will be ignored. It is retained for backwards compatibility.
	BufferSize int64
}

// DefaultDownloadSpec is used when 'spec' argument to DownloadImage() is nil
var DefaultDownloadSpec = &Downloader{Concurrency: 4, PartSize: 64 * 1024}

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

// DownloadImage implements a multi-part (concurrent) downloader for
// Cloud Library images. spec is used to define transfer parameters. pb is an
// optional progress bar interface.  If pb is nil, NoopProgressBar is used.
//
// The downloader will handle source files of all sizes and is not limited to
// only files larger than Downloader.PartSize. It will automatically adjust the
// concurrency for source files that do not meet minimum size for multi-part
// downloads.
func (c *Client) DownloadImage(ctx context.Context, dst io.WriterAt, arch, path, tag string, spec *Downloader, pb ProgressBar) error {
	if pb == nil {
		pb = &NoopProgressBar{}
	}

	if spec == nil {
		spec = DefaultDownloadSpec
	}

	if strings.Contains(path, ":") {
		return fmt.Errorf("malformed image path: %s", path)
	}

	name := strings.TrimPrefix(path, "/")
	if tag == "" {
		tag = "latest"
	}

	// Attempt to download from OCI registry directly
	if err := c.ociDownloadImage(ctx, arch, name, tag, dst, spec, pb); err != nil {
		if !errors.Is(err, errOCIDownloadNotSupported) {
			return err
		}

		c.logger.Log("Fallback to (legacy) library download")

		return c.libraryDownloadImage(ctx, arch, name, tag, dst, spec, pb)
	}
	return nil
}

func (c *Client) libraryDownloadImage(ctx context.Context, arch, name, tag string, dst io.WriterAt, spec *Downloader, pb ProgressBar) error {
	if arch != "" && !c.apiAtLeast(ctx, APIVersionV2ArchTags) {
		c.logger.Log("This library does not support architecture specific tags")
		c.logger.Log("The image returned may not be the requested architecture")
	}

	// Create custom HTTP client for handling (potential) redirection from 'v1/imagefile'
	httpClient := &http.Client{
		Transport: c.httpClient.Transport,
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
		Jar:     c.httpClient.Jar,
		Timeout: c.httpClient.Timeout,
	}

	var creds credentials
	if c.authToken != "" {
		// Only include credentials if redirected to same host as base URL
		creds = bearerTokenCredentials{authToken: c.authToken}
	}

	q := url.Values{}
	q.Add("arch", arch)

	u := c.baseURL.ResolveReference(&url.URL{Path: fmt.Sprintf("v1/imagefile/%v:%v", name, tag), RawQuery: q.Encode()})

	c.logger.Logf("Performing initial pull request from %v", u.String())

	for {
		// Perform ranged GET request to get first part of image
		req, err := c.newRangeGetRequest(ctx, creds, u.String(), 0, spec.PartSize-1)
		if err != nil {
			return err
		}
		res, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()

		switch res.StatusCode {
		case http.StatusOK:
			// HTTP server does not handle HTTP range requests
			c.logger.Log("Server does not support HTTP range requests, falling back to single stream download")

			size, err := parseContentLengthHeader(res.Header.Get("Content-Length"))
			if err != nil {
				return err
			}

			return c.handleSinglePartDownloadResponse(ctx, dst, res.Body, size, pb)
		case http.StatusSeeOther:
			u, err = url.Parse(res.Header.Get("Location"))
			if err != nil {
				return err
			}

			c.logger.Logf("Redirected to %v", u.String())

			if creds != nil && !samehost(c.baseURL, u) {
				// Only include credentials if redirected to same host as base URL
				creds = nil
			}

			// Use default HTTP client for redirect
			httpClient = c.httpClient

			// Reattempt GET request to redirected URL
			continue
		case http.StatusPartialContent:
			return c.handleMultipartDownloadResponse(ctx, res, dst, creds, u, spec, pb)
		default:
			return c.requestErrorHandler(res.StatusCode)
		}
	}
}

func (c *Client) requestErrorHandler(code int) error {
	switch code {
	case http.StatusNotFound:
		return errImageNotFound
	case http.StatusUnauthorized:
		return errUnauthorized
	default:
		return fmt.Errorf("unexpected HTTP status %d", code)
	}
}

func (c *Client) handleMultipartDownloadResponse(ctx context.Context, res *http.Response, dst io.WriterAt, creds credentials, u *url.URL, spec *Downloader, pb ProgressBar) error {
	size, err := parseContentRange(res.Header.Get("Content-Range"))
	if err != nil {
		return err
	}

	// Initialize progress bar
	pb.Init(size)
	defer pb.Wait()

	defer func() {
		// Ensure progress bar is properly aborted in the event of an error
		if err != nil {
			pb.Abort(true)
		}
	}()

	// Write first part to dst
	if _, err = io.Copy(&filePartDescriptor{start: 0, end: size - 1, w: dst}, res.Body); err != nil {
		return err
	}

	// Continue with successive parts (part number >= 2)
	return c.downloadParts(ctx, u.String(), creds, dst, size, spec, 1, pb)
}

func (c *Client) newRangeGetRequest(ctx context.Context, creds credentials, u string, start, end int64) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	if creds != nil {
		if err := creds.ModifyRequest(req); err != nil {
			return nil, err
		}
	}

	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	return req, nil
}

// samehost returns true if host1 and host2 are, in fact, the same host by
// comparing scheme (https == https) and host, including port.
//
// Hosts will be treated as dissimilar if one host includes domain suffix
// and the other does not, even if the host names match.
func samehost(host1, host2 *url.URL) bool {
	return strings.EqualFold(host1.Scheme, host2.Scheme) && strings.EqualFold(host1.Host, host2.Host)
}

func parseContentLengthHeader(val string) (int64, error) {
	if val == "" {
		return int64(-1), nil
	}
	size, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return -1, fmt.Errorf("parsing Content-Length header %v: %v", val, err)
	}
	return size, nil
}

// handleSinglePartDownloadResponse implements a simple, single stream downloader
func (c *Client) handleSinglePartDownloadResponse(ctx context.Context, w io.WriterAt, r io.Reader, size int64, pb ProgressBar) error {
	pb.Init(size)
	defer pb.Wait()

	proxyReader := pb.ProxyReader(r)
	defer proxyReader.Close()

	written, err := io.Copy(&filePartDescriptor{start: 0, end: size - 1, w: w}, proxyReader)
	if err != nil {
		pb.Abort(true)

		return err
	}

	c.logger.Logf("Downloaded %v byte(s)", written)

	return nil
}
