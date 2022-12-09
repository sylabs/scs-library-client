// Copyright (c) 2018-2022, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// ociRegistryAuth uses Cloud Library endpoint to determine if artifact can be pulled
// directly from OCI registry.
//
// Returns url and credentials (if applicable) for that url.
func (c *Client) ociRegistryAuth(ctx context.Context, name, tag, arch string, accessTypes []accessType) (*url.URL, *bearerTokenCredentials, error) {
	// Build raw query string to get token for specified namespace and access
	v := url.Values{}
	elems := strings.Split(name, "/")
	v.Set("namespace", fmt.Sprintf("%v/%v", elems[0], elems[1]))

	ats := make([]string, 0, len(accessTypes))
	for _, at := range accessTypes {
		ats = append(ats, string(at))
	}

	v.Set("accessTypes", strings.Join(ats, ","))

	req, err := c.newRequest(ctx, http.MethodGet, "v1/oci-redirect", v.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}

	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("error determining direct OCI registry access: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("error determining direct OCI registry access: %w", err)
	}

	type ociDownloadRedirectResponse struct {
		Token       string `json:"token"`
		RegistryURI string `json:"url"`
	}

	var ociArtifactSpec ociDownloadRedirectResponse

	if err := json.NewDecoder(res.Body).Decode(&ociArtifactSpec); err != nil {
		return nil, nil, fmt.Errorf("error decoding direct OCI registry access response: %w", err)
	}

	endpoint, err := url.Parse(ociArtifactSpec.RegistryURI)
	if err != nil {
		return nil, nil, fmt.Errorf("malformed OCI registry URI %v: %v", ociArtifactSpec.RegistryURI, err)
	}
	return endpoint, &bearerTokenCredentials{authToken: ociArtifactSpec.Token}, nil
}

const (
	mediaTypeSIFConfig = "application/vnd.sylabs.sif.config.v1+json"
)

type imageConfig struct {
	Architecture string        `json:"architecture"`
	OS           string        `json:"os"`
	RootFS       digest.Digest `json:"rootfs"`
	Description  string        `json:"description,omitempty"`
	Signed       bool          `json:"signed"`
	Encrypted    bool          `json:"encrypted"`
}

type credentials interface {
	ModifyRequest(r *http.Request, opts ...modifyRequestOption) error
}

type basicCredentials struct {
	username string
	password string
}

func (c basicCredentials) ModifyRequest(r *http.Request, opts ...modifyRequestOption) error {
	r.SetBasicAuth(c.username, c.password)
	return nil
}

type bearerTokenCredentials struct {
	authToken string
}

func (c bearerTokenCredentials) ModifyRequest(r *http.Request, opts ...modifyRequestOption) error {
	r.Header.Set("Authorization", fmt.Sprintf("Bearer %v", c.authToken))
	return nil
}

type accessType string

const accessTypePull accessType = "pull"

type accessOptions struct {
	namespace   string
	accessTypes []accessType
}

type ociRegistry struct {
	baseURL    *url.URL
	httpClient *http.Client
	userAgent  string
}

var errArchNotSpecified = errors.New("architecture not specified")

func (r *ociRegistry) getManifestFromIndex(idx v1.Index, arch string) (digest.Digest, error) {
	// If arch not supplied, return single manifest or error.
	if arch == "" {
		if len(idx.Manifests) != 1 || idx.Manifests[0].MediaType != v1.MediaTypeImageManifest {
			return "", errArchNotSpecified
		}

		return idx.Manifests[0].Digest, nil
	}

	// Otherwise, go fish for matching architecture/OS.
	for _, m := range idx.Manifests {
		// Only consider image manifests.
		if m.MediaType != v1.MediaTypeImageManifest {
			continue
		}

		// If arch matches, execute!
		if m.Platform.Architecture == arch {
			return m.Digest, nil
		}
	}

	// If we make it here, no matching OS/architecture was found.
	return "", fmt.Errorf("no matching OS/architecture (%v) found", arch)
}

func (r *ociRegistry) getImageManifest(ctx context.Context, c credentials, name, tag, arch string) (digest.Digest, v1.Manifest, error) {
	if _, idx, err := r.DownloadV1Index(ctx, c, name, tag); err == nil {
		// Get manifest from index
		d, err := r.getManifestFromIndex(idx, arch)
		if err != nil {
			return "", v1.Manifest{}, err
		}

		tag = d.String()
	}

	return r.downloadV1Manifest(ctx, c, name, tag)
}

func (r *ociRegistry) getImageDetails(ctx context.Context, c credentials, name, tag, arch string) (v1.Descriptor, error) {
	_, m, err := r.getImageManifest(ctx, c, name, tag, arch)
	if err != nil {
		return v1.Descriptor{}, err
	}

	if got, want := m.Config.MediaType, mediaTypeSIFConfig; got != want {
		return v1.Descriptor{}, fmt.Errorf("unexpected media type error (got %v, want %v)", got, want)
	}

	// There should always be exactly one layer (the image blob).
	if n := len(m.Layers); n != 1 {
		return v1.Descriptor{}, fmt.Errorf("unexpected # of layers: %v", n)
	}

	// If architecture was supplied, ensure the image config matches.
	ic, err := r.getImageConfig(ctx, c, name, m.Config.Digest)
	if err != nil {
		return v1.Descriptor{}, err
	}

	// Ensure architecture matches, if supplied.
	if got, want := ic.Architecture, arch; want != "" && got != want {
		return v1.Descriptor{}, &unexpectedArchitectureError{got, want}
	}

	return m.Layers[0], nil
}

func (r *ociRegistry) DownloadV1Index(ctx context.Context, c credentials, name, tag string) (digest.Digest, v1.Index, error) {
	var idx v1.Index
	d, err := r.downloadManifest(ctx, c, name, tag, &idx, v1.MediaTypeImageIndex)
	return d, idx, err
}

func (r *ociRegistry) downloadV1Manifest(ctx context.Context, c credentials, name, tag string) (digest.Digest, v1.Manifest, error) {
	var m v1.Manifest
	d, err := r.downloadManifest(ctx, c, name, tag, &m, v1.MediaTypeImageManifest)
	return d, m, err
}

func (r *ociRegistry) newRequest(ctx context.Context, method string, u *url.URL, body io.Reader) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, r.baseURL.ResolveReference(u).String(), body)
}

type modifyRequestOptions struct {
	httpClient         *http.Client
	userAgent          string
	authenticateHeader authHeader // Parsed "Www-Authenticate" header
	accessOptions      *accessOptions
}

type modifyRequestOption func(*modifyRequestOptions) error

// withAuthenticateHeader specifies s as the value of the "Www-Authenticate" header.
func withAuthenticateHeader(s string) modifyRequestOption {
	return func(opts *modifyRequestOptions) error {
		ah, err := parseAuthHeader(s)
		if err != nil {
			return err
		}
		opts.authenticateHeader = ah

		return nil
	}
}

type authType int

const (
	authTypeUnknown authType = iota
	authTypeBasic
	authTypeBearer
)

// parseList parses a comma-separated list of values as described by RFC 2068 and returns list
// elements.
//
// Lifted from https://code.google.com/p/gorilla/source/browse/http/parser/parser.go
func parseList(value string) []string {
	var list []string
	var escape, quote bool
	b := bytes.Buffer{}

	for _, r := range value {
		switch {
		case escape:
			b.WriteRune(r)
			escape = false
		case quote:
			if r == '\\' {
				escape = true
			} else {
				if r == '"' {
					quote = false
				}
				b.WriteRune(r)
			}
		case r == ',':
			list = append(list, strings.TrimSpace(b.String()))
			b.Reset()
		case r == '"':
			quote = true
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}

	// Append last part.
	if s := b.String(); s != "" {
		list = append(list, strings.TrimSpace(s))
	}

	return list
}

// parsePairs extracts key/value pairs from a comma-separated list of values as described by RFC
// 2068 and returns a map[key]value. The resulting values are unquoted. If a list element doesn't
// contain a "=", the key is the element itself and the value is an empty string.
//
// Lifted from https://code.google.com/p/gorilla/source/browse/http/parser/parser.go
func parsePairs(value string) map[string]string {
	m := make(map[string]string)

	for _, pair := range parseList(strings.TrimSpace(value)) {
		if i := strings.Index(pair, "="); i < 0 {
			m[pair] = ""
		} else {
			v := pair[i+1:]
			if v[0] == '"' && v[len(v)-1] == '"' {
				// Unquote it.
				v = v[1 : len(v)-1]
			}
			m[pair[:i]] = v
		}
	}

	return m
}

var errInvalidAuthHeader = errors.New("invalid auth header")

type authHeader struct {
	at      authType
	realm   string
	service string
	scope   string
}

type unknownAuthTypeError struct {
	authType string
}

func (e *unknownAuthTypeError) Error() string {
	if e.authType != "" {
		return fmt.Sprintf("unknown auth type '%v'", e.authType)
	}
	return "unknown auth type"
}

func (e *unknownAuthTypeError) Is(target error) bool {
	var t *unknownAuthTypeError
	if errors.As(target, &t) {
		return t.authType == "" || e.authType == t.authType
	}
	return false
}

func getAuthType(raw string) (authType, error) {
	switch {
	case strings.EqualFold(raw, "basic"):
		return authTypeBasic, nil
	case strings.EqualFold(raw, "bearer"):
		return authTypeBearer, nil
	default:
		return authTypeUnknown, &unknownAuthTypeError{raw}
	}
}

func parseAuthHeader(authenticateHeader string) (authHeader, error) {
	parts := strings.SplitN(authenticateHeader, " ", 2)

	if len(parts) != 2 {
		return authHeader{}, fmt.Errorf("%w: %v", errInvalidAuthHeader, authenticateHeader)
	}

	authType, err := getAuthType(parts[0])
	if err != nil {
		return authHeader{}, err
	}

	ah := authHeader{at: authType}
	pairs := parsePairs(parts[1])

	if v, ok := pairs["realm"]; ok {
		ah.realm = v
	}
	if v, ok := pairs["service"]; ok {
		ah.service = v
	}
	if v, ok := pairs["scope"]; ok {
		ah.scope = v
	}

	return ah, nil
}

type noneCreds struct{}

func (c *noneCreds) ModifyRequest(r *http.Request, opts ...modifyRequestOption) error {
	r.Header.Set("Authorization", "none")

	return nil
}

// none returns Credentials that set the authorization header to "none".
func none() *noneCreds {
	return &noneCreds{}
}

func withUserAgent(s string) modifyRequestOption {
	return func(o *modifyRequestOptions) error {
		o.userAgent = s
		return nil
	}
}

func withHTTPClient(client *http.Client) modifyRequestOption {
	return func(o *modifyRequestOptions) error {
		o.httpClient = client
		return nil
	}
}

var errResetHTTPBody = errors.New("unable to reset HTTP request body")

func (r *ociRegistry) doRequestWithCredentials(req *http.Request, c credentials, opts ...modifyRequestOption) (*http.Response, error) {
	opts = append(opts,
		withUserAgent(r.userAgent),
		withHTTPClient(r.httpClient),
	)

	// Modify request to include credentials.
	if err := c.ModifyRequest(req, opts...); err != nil {
		return nil, err
	}

	res, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if code := res.StatusCode; code/100 != 2 {
		defer res.Body.Close()

		return nil, fmt.Errorf("unexpected HTTP status %v", res.StatusCode)
	}

	return res, nil
}

func (r *ociRegistry) retryRequestWithCredentials(req *http.Request, c credentials, opts ...modifyRequestOption) (*http.Response, error) {
	// If the original request contained a body, we need to reset it.
	if req.Body != nil {
		if req.GetBody == nil {
			return nil, errResetHTTPBody
		}

		rc, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		req.Body = rc
	}

	return r.doRequestWithCredentials(req, c, opts...)
}

func (r *ociRegistry) doRequest(req *http.Request, c credentials, opts ...modifyRequestOption) (*http.Response, error) {
	res, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if code := res.StatusCode; code/100 != 2 {
		defer res.Body.Close()

		// If authorization required, re-attempt request using credentials (if supplied) according
		// to the contents of the "WWW-Authenticate" header (if present).
		if code == http.StatusUnauthorized {
			if c == nil {
				// Unauthenticated requests to certain Harbor APIs require an Authorization header,
				// even if it's set to "none". 🤦
				c = none()
			}

			opts = append(opts, withAuthenticateHeader(res.Header.Get("WWW-Authenticate")))
			return r.retryRequestWithCredentials(req, c, opts...)
		}

		return nil, fmt.Errorf("unexpected http status %v", code)
	}

	return res, nil
}

// withNamespaceAccess specifies that credentials must be procured that are sufficient to grant the
// access specified by accessTypes to namespace name.
func withNamespaceAccess(name string, accessTypes ...accessType) modifyRequestOption {
	return func(opts *modifyRequestOptions) error {
		opts.accessOptions = &accessOptions{
			namespace:   name,
			accessTypes: accessTypes,
		}

		return nil
	}
}

type unexpectedContentTypeError struct {
	got  string
	want string
}

func (e *unexpectedContentTypeError) Error() string {
	return fmt.Sprintf("unexpected content type: got %v, want %v", e.got, e.want)
}

// downloadManifest downloads the manifest of type contentType associated with name/ref in the
// registry, and unmarshals it to v.
func (r *ociRegistry) downloadManifest(ctx context.Context, c credentials, name, tag string, v interface{}, contentType string) (digest.Digest, error) {
	req, err := r.newRequest(ctx, http.MethodGet, &url.URL{Path: fmt.Sprintf("v2/%v/manifests/%v", name, tag)}, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", contentType)

	res, err := r.doRequest(req, c, withNamespaceAccess(name, accessTypePull))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	// Although we've set the "Accept" header, some registries will return other content types.
	if got, want := res.Header.Get("Content-Type"), contentType; got != want {
		return "", &unexpectedContentTypeError{got, want}
	}

	d := digest.Digest(res.Header.Get("Docker-Content-Digest"))

	if err := d.Validate(); err != nil {
		return "", err
	}

	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		return "", err
	}
	return d, nil
}

func (r *ociRegistry) downloadBlob(ctx context.Context, c credentials, name string, d digest.Digest, rangeValue string, w io.Writer) (int64, error) {
	if err := d.Validate(); err != nil {
		return 0, err
	}

	req, err := r.newRequest(ctx, http.MethodGet, &url.URL{Path: fmt.Sprintf("v2/%v/blobs/%v", name, d)}, nil)
	if err != nil {
		return 0, err
	}

	// Set HTTP Range header, if applicable.
	if rangeValue != "" {
		req.Header.Set("Range", rangeValue)
	}

	res, err := r.doRequest(req, c, withNamespaceAccess(name, accessTypePull))
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	// Download blob.
	return io.Copy(w, res.Body)
}

var errArchitectureNotPresent = errors.New("architecture not present")

// validateImageConfig validates ic, and returns an error when ic is invalid.
func validateImageConfig(ic imageConfig) error {
	if ic.Architecture == "" {
		return errArchitectureNotPresent
	}

	return ic.RootFS.Validate()
}

type unexpectedArchitectureError struct {
	got  string
	want string
}

func (e *unexpectedArchitectureError) Error() string {
	return fmt.Sprintf("unexpected image architecture: got %v, want %v", e.got, e.want)
}

func (e *unexpectedArchitectureError) Is(target error) bool {
	t := &unexpectedArchitectureError{}
	if !errors.As(target, &t) {
		return false
	}
	return (e.got == t.got || t.got == "") &&
		(e.want == t.want || t.want == "")
}

var errDigestNotVerified = errors.New("digest not verified")

func (r *ociRegistry) getImageConfig(ctx context.Context, c credentials, name string, d digest.Digest) (imageConfig, error) {
	var b bytes.Buffer
	if _, err := r.downloadBlob(ctx, c, name, d, "", &b); err != nil {
		return imageConfig{}, err
	}

	if digest.FromBytes(b.Bytes()) != d {
		return imageConfig{}, errDigestNotVerified
	}

	var ic imageConfig
	if err := json.Unmarshal(b.Bytes(), &ic); err != nil {
		return imageConfig{}, err
	}

	if err := validateImageConfig(ic); err != nil {
		return imageConfig{}, fmt.Errorf("invalid image config: %w", err)
	}

	return ic, nil
}

var errOCIDownloadNotSupported = errors.New("not supported")

func (c *Client) ociDownloadImage(ctx context.Context, arch, name, tag string, w io.WriterAt, spec *Downloader, pb ProgressBar) error {
	// Attempt to obtain (direct) OCI registry auth token
	registryURI, creds, err := c.ociRegistryAuth(ctx, name, tag, arch, []accessType{accessTypePull})
	if err != nil {
		return errOCIDownloadNotSupported
	}

	// Download directly from OCI registry
	c.Logger.Logf("Using OCI registry endpoint %v", registryURI)

	reg := &ociRegistry{
		baseURL:    registryURI,
		httpClient: c.HTTPClient,
	}

	// Fetch image manifest to get image details
	id, err := reg.getImageDetails(ctx, creds, name, tag, arch)
	if err != nil {
		return fmt.Errorf("error getting image details: %w", err)
	}

	imageURI := registryURI.ResolveReference(&url.URL{Path: fmt.Sprintf("v2/%v/blobs/%v", name, id.Digest)}).String()

	return c.multipartDownload(ctx, imageURI, creds, w, id.Size, spec, pb)
}
