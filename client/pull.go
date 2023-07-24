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
	"os"
	"strconv"
	"strings"

	jsonresp "github.com/sylabs/json-resp"
)

var errRequestedImageNotFound = fmt.Errorf("requested image was not found in the library")

// DownloadImage will retrieve an image from the Container Library, saving it
// into the specified io.Writer. The timeout value for this operation is set
// within the context. It is recommended to use a large value (ie. 1800 seconds)
// to prevent timeout when downloading large images.
func (c *Client) DownloadImage(ctx context.Context, w io.Writer, arch, path, tag string, callback func(int64, io.Reader, io.Writer) error) error {
	if arch != "" && !c.apiAtLeast(ctx, APIVersionV2ArchTags) {
		c.Logger.Log("This library does not support architecture specific tags")
		c.Logger.Log("The image returned may not be the requested architecture")
	}

	if strings.Contains(path, ":") {
		return fmt.Errorf("%w: malformed image path: %s", errBadRequest, path)
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
		return errRequestedImageNotFound
	}

	if res.StatusCode != http.StatusOK {
		err := jsonresp.ReadError(res.Body)
		if err != nil {
			return fmt.Errorf("download did not succeed: %w", err)
		}
		if res.StatusCode == http.StatusUnauthorized {
			return ErrUnauthorized
		}
		return fmt.Errorf("%w: unexpected http status code: %d", errHTTP, res.StatusCode)
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

	if strings.Contains(path, ":") {
		return fmt.Errorf("%w: malformed image path: %s", errBadRequest, path)
	}

	name := strings.TrimPrefix(path, "/")
	if tag == "" {
		tag = "latest"
	}

	// Check for direct OCI registry access
	if err := c.ociDownloadImage(ctx, arch, name, tag, dst, spec, pb); err != nil {
		if !errors.Is(err, errOCIDownloadNotSupported) {
			// Return OCI download error or fallback to legacy download
			return err
		}

		c.Logger.Log("Fallback to (legacy) library download")

		return c.legacyDownloadImage(ctx, arch, name, tag, dst, spec, pb)
	}
	return nil
}

func (c *Client) legacyDownloadImage(ctx context.Context, arch, name, tag string, dst io.WriterAt, spec *Downloader, pb ProgressBar) error {
	if arch != "" && !c.apiAtLeast(ctx, APIVersionV2ArchTags) {
		c.Logger.Log("This library does not support architecture specific tags")
		c.Logger.Log("The image returned may not be the requested architecture")
	}

	apiPath := fmt.Sprintf("v1/imagefile/%v:%v", name, tag)
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
				return fmt.Errorf("%w: stopped after %d redirects", errHTTP, maxRedir)
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
		return errRequestedImageNotFound
	}

	if res.StatusCode == http.StatusOK {
		// Library endpoint does not provide HTTP redirection response, treat as single stream download

		c.Logger.Log("Library endpoint does not support concurrent downloads; reverting to single stream")

		size, err := parseContentLengthHeader(res.Header.Get("Content-Length"))
		if err != nil {
			return err
		}

		return c.download(ctx, dst, res.Body, size, pb)
	}

	if res.StatusCode != http.StatusSeeOther {
		if res.StatusCode == http.StatusUnauthorized {
			return ErrUnauthorized
		}
		return fmt.Errorf("%w: unexpected http status %d", errHTTP, res.StatusCode)
	}

	// Get image metadata to determine image size
	img, err := c.GetImage(ctx, arch, fmt.Sprintf("%v:%v", name, tag))
	if err != nil {
		return err
	}

	redirectURL, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		return err
	}

	var creds credentials
	if c.AuthToken != "" && samehost(c.BaseURL, redirectURL) {
		// Only include credentials if redirected to same host as base URL
		creds = bearerTokenCredentials{authToken: c.AuthToken}
	}

	// Use redirect URL to download artifact
	return c.multipartDownload(ctx, redirectURL.String(), creds, dst, img.Size, spec, pb)
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
		return -1, fmt.Errorf("parsing Content-Length header %v: %w", val, err)
	}
	return size, nil
}

// download implements a simple, single stream downloader
func (c *Client) download(_ context.Context, w io.WriterAt, r io.Reader, size int64, pb ProgressBar) error {
	pb.Init(size)
	defer pb.Wait()

	proxyReader := pb.ProxyReader(r)
	defer proxyReader.Close()

	written, err := io.Copy(&filePartDescriptor{start: 0, end: size - 1, w: w}, proxyReader)
	if err != nil {
		pb.Abort(true)

		return err
	}

	c.Logger.Logf("Downloaded %v byte(s)", written)

	return nil
}
