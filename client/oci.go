// Copyright (c) 2018-2023, Sylabs Inc. All rights reserved.
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
	"strconv"
	"strings"
	"time"

	"github.com/go-log/log"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sylabs/sif/v2/pkg/sif"
)

const mediaTypeSIFLayer = "application/vnd.sylabs.sif.layer.v1.sif"

// ociRegistryAuth uses Cloud Library endpoint to determine if artifact can be pulled
// directly from OCI registry.
//
// Returns url, credentials (if applicable) for that url, and mapped name.
//
// The mapped name can be the same value as 'name' or mapped to a fully-qualified name
// (ie. from "alpine" to "library/default/alpine") if supported by cloud library server.
// It will never be an empty string ("")
func (c *Client) ociRegistryAuth(ctx context.Context, name string, accessTypes []accessType) (*url.URL, *bearerTokenCredentials, string, error) {
	// Build raw query string to get token for specified namespace and access
	v := url.Values{}
	v.Set("namespace", name)

	// Setting 'mapped' to '1' (true) enables support for mapping short library refs to
	// fully-qualified name
	v.Set("mapped", strconv.Itoa(1))

	ats := make([]string, 0, len(accessTypes))
	for _, at := range accessTypes {
		ats = append(ats, string(at))
	}

	v.Set("accessTypes", strings.Join(ats, ","))

	req, err := c.newRequest(ctx, http.MethodGet, "v1/oci-redirect", v.Encode(), nil)
	if err != nil {
		return nil, nil, "", err
	}

	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, "", fmt.Errorf("error determining direct OCI registry access: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, nil, "", fmt.Errorf("error determining direct OCI registry access: %w", err)
	}

	type ociDownloadRedirectResponse struct {
		Token       string `json:"token"`
		RegistryURI string `json:"url"`
		Name        string `json:"name"`
	}

	var ociArtifactSpec ociDownloadRedirectResponse

	if err := json.NewDecoder(res.Body).Decode(&ociArtifactSpec); err != nil {
		return nil, nil, "", fmt.Errorf("error decoding direct OCI registry access response: %w", err)
	}

	if ociArtifactSpec.Name != "" && ociArtifactSpec.Name != name {
		name = ociArtifactSpec.Name
	}

	endpoint, err := url.Parse(ociArtifactSpec.RegistryURI)
	if err != nil {
		return nil, nil, "", fmt.Errorf("malformed OCI registry URI %v: %w", ociArtifactSpec.RegistryURI, err)
	}
	return endpoint, &bearerTokenCredentials{authToken: ociArtifactSpec.Token}, name, nil
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

func (c basicCredentials) ModifyRequest(r *http.Request, _ ...modifyRequestOption) error {
	r.SetBasicAuth(c.username, c.password)
	return nil
}

type bearerTokenCredentials struct {
	authToken string
}

func (c bearerTokenCredentials) ModifyRequest(r *http.Request, _ ...modifyRequestOption) error {
	if c.authToken != "" {
		r.Header.Set("Authorization", fmt.Sprintf("Bearer %v", c.authToken))
	}
	return nil
}

type accessType string

const (
	accessTypePull accessType = "pull"
	accessTypePush accessType = "push"
)

type accessOptions struct {
	namespace   string
	accessTypes []accessType
}

type ociRegistry struct {
	baseURL    *url.URL
	httpClient *http.Client
	userAgent  string
	logger     log.Logger
}

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
	return "", fmt.Errorf("%w: no matching OS/architecture (%v) found", errOCIRegistry, arch)
}

func (r *ociRegistry) getImageManifest(ctx context.Context, creds credentials, name, tag, arch string) (digest.Digest, v1.Manifest, error) {
	if _, idx, err := r.DownloadV1Index(ctx, creds, name, tag); err == nil {
		// Get manifest from index
		d, err := r.getManifestFromIndex(idx, arch)
		if err != nil {
			return "", v1.Manifest{}, err
		}

		tag = d.String()
	}

	return r.downloadV1Manifest(ctx, creds, name, tag)
}

func (r *ociRegistry) getImageDetails(ctx context.Context, creds credentials, name, tag, arch string) (v1.Descriptor, error) {
	_, m, err := r.getImageManifest(ctx, creds, name, tag, arch)
	if err != nil {
		return v1.Descriptor{}, err
	}

	if got, want := m.Config.MediaType, mediaTypeSIFConfig; got != want {
		return v1.Descriptor{}, fmt.Errorf("%w: unexpected media type error (got %v, want %v)", errOCIRegistry, got, want)
	}

	// There should always be exactly one layer (the image blob).
	if n := len(m.Layers); n != 1 {
		return v1.Descriptor{}, fmt.Errorf("%w: unexpected # of layers: %v", errOCIRegistry, n)
	}

	// If architecture was supplied, ensure the image config matches.
	ic, err := r.getImageConfig(ctx, creds, name, m.Config.Digest)
	if err != nil {
		return v1.Descriptor{}, err
	}

	// Ensure architecture matches, if supplied.
	if got, want := ic.Architecture, arch; want != "" && got != want {
		return v1.Descriptor{}, &unexpectedArchitectureError{got, want}
	}

	return m.Layers[0], nil
}

func (r *ociRegistry) DownloadV1Index(ctx context.Context, creds credentials, name, tag string) (digest.Digest, v1.Index, error) {
	var idx v1.Index
	d, err := r.downloadManifest(ctx, creds, name, tag, &idx, v1.MediaTypeImageIndex)
	return d, idx, err
}

func (r *ociRegistry) downloadV1Manifest(ctx context.Context, creds credentials, name, tag string) (digest.Digest, v1.Manifest, error) {
	var m v1.Manifest
	d, err := r.downloadManifest(ctx, creds, name, tag, &m, v1.MediaTypeImageManifest)
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

func (c *noneCreds) ModifyRequest(r *http.Request, _ ...modifyRequestOption) error {
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

func (r *ociRegistry) doRequestWithCredentials(req *http.Request, creds credentials, opts ...modifyRequestOption) (*http.Response, error) {
	opts = append(opts,
		withUserAgent(r.userAgent),
		withHTTPClient(r.httpClient),
	)

	// Modify request to include credentials.
	if err := creds.ModifyRequest(req, opts...); err != nil {
		return nil, err
	}

	res, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if code := res.StatusCode; code/100 != 2 {
		defer res.Body.Close()

		if code == http.StatusUnauthorized {
			return nil, ErrUnauthorized
		}

		return nil, fmt.Errorf("%w: unexpected http status %v", errHTTP, res.StatusCode)
	}

	return res, nil
}

func (r *ociRegistry) retryRequestWithCredentials(req *http.Request, creds credentials, opts ...modifyRequestOption) (*http.Response, error) {
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

	return r.doRequestWithCredentials(req, creds, opts...)
}

func (r *ociRegistry) doRequest(req *http.Request, creds credentials, opts ...modifyRequestOption) (*http.Response, error) {
	res, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if code := res.StatusCode; code/100 != 2 {
		defer res.Body.Close()

		// If authorization required, re-attempt request using credentials (if supplied) according
		// to the contents of the "WWW-Authenticate" header (if present).
		if code == http.StatusUnauthorized {
			if creds == nil {
				// Unauthenticated requests to certain Harbor APIs require an Authorization header,
				// even if it's set to "none". 🤦
				creds = none()
			}

			opts = append(opts, withAuthenticateHeader(res.Header.Get("WWW-Authenticate")))
			return r.retryRequestWithCredentials(req, creds, opts...)
		}

		if code == http.StatusUnauthorized {
			return nil, ErrUnauthorized
		}

		return nil, fmt.Errorf("%w: unexpected http status %v", errHTTP, code)
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
func (r *ociRegistry) downloadManifest(ctx context.Context, creds credentials, name, tag string, v interface{}, contentType string) (digest.Digest, error) {
	req, err := r.newRequest(ctx, http.MethodGet, &url.URL{Path: fmt.Sprintf("v2/%v/manifests/%v", name, tag)}, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", contentType)

	res, err := r.doRequest(req, creds, withNamespaceAccess(name, accessTypePull))
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

func (r *ociRegistry) downloadBlob(ctx context.Context, creds credentials, name string, d digest.Digest, rangeValue string, w io.Writer) (int64, error) {
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

	res, err := r.doRequest(req, creds, withNamespaceAccess(name, accessTypePull))
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	// Download blob.
	return io.Copy(w, res.Body)
}

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

func (r *ociRegistry) getImageConfig(ctx context.Context, creds credentials, name string, d digest.Digest) (imageConfig, error) {
	var b bytes.Buffer
	if _, err := r.downloadBlob(ctx, creds, name, d, "", &b); err != nil {
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

// newOCIRegistry returns *ociRegistry, credentials for that registry, and the (optionally) remapped image name
func (c *Client) newOCIRegistry(ctx context.Context, name string, accessTypes []accessType) (*ociRegistry, *bearerTokenCredentials, string, error) {
	// Attempt to obtain (direct) OCI registry auth token
	originalName := name

	registryURI, creds, name, err := c.ociRegistryAuth(ctx, name, accessTypes)
	if err != nil {
		return nil, nil, "", errOCIDownloadNotSupported
	}

	// Download directly from OCI registry
	c.Logger.Logf("Using OCI registry endpoint %v", registryURI)

	if name != "" && originalName != name {
		c.Logger.Logf("OCI artifact name \"%v\" mapped to \"%v\"", originalName, name)
	}

	return &ociRegistry{baseURL: registryURI, httpClient: c.HTTPClient, logger: c.Logger}, creds, name, nil
}

func (c *Client) ociDownloadImage(ctx context.Context, arch, name, tag string, w io.WriterAt, spec *Downloader, pb ProgressBar) error {
	reg, creds, name, err := c.newOCIRegistry(ctx, name, []accessType{accessTypePull})
	if err != nil {
		return err
	}

	// Fetch image manifest to get image details
	id, err := reg.getImageDetails(ctx, creds, name, tag, arch)
	if err != nil {
		return fmt.Errorf("error getting image details: %w", err)
	}

	imageURI := reg.baseURL.ResolveReference(&url.URL{Path: fmt.Sprintf("v2/%v/blobs/%v", name, id.Digest)}).String()

	return c.multipartDownload(ctx, imageURI, creds, w, id.Size, spec, pb)
}

const sifHeaderSize = 32768

type unexpectedImageDigest struct {
	got  digest.Digest
	want digest.Digest
}

func (e *unexpectedImageDigest) Error() string {
	return fmt.Sprintf("unexpected image digest: %v != %v", e.got, e.want)
}

func (c *Client) ociUploadImage(ctx context.Context, r io.Reader, size int64, name, _ string, tags []string, description, hash string, callback UploadCallback) error {
	reg, creds, name, err := c.newOCIRegistry(ctx, name, []accessType{accessTypePull, accessTypePush})
	if err != nil {
		return err
	}

	sifHeader := bytes.NewBuffer(make([]byte, 0, sifHeaderSize))

	// Convert SIF hash to OCI digest.
	imageDigest := digest.Digest(strings.ReplaceAll(hash, ".", ":"))
	if err := imageDigest.Validate(); err != nil {
		return fmt.Errorf("invalid image hash '%v': %w", hash, err)
	}

	// Check if image exists, 'ok' is set correctly if this returns an error.
	ok, _ := reg.existingImageBlob(ctx, creds, name, imageDigest)

	var id digest.Digest

	if !ok {
		// Construct a reader that tees off a copy of the SIF header into a buffer as the blob is uploaded.
		r = io.MultiReader(
			io.TeeReader(io.LimitReader(r, sifHeaderSize), sifHeader),
			r,
		)

		if callback != nil {
			callback.InitUpload(size, r)

			r = callback.GetReader()
		}

		var err error
		id, _, err = reg.uploadImageBlob(ctx, creds, name, size, r)
		if err != nil {
			if callback != nil {
				callback.Terminate()
			}

			return fmt.Errorf("upload image blob failed: %w", err)
		}

		if callback != nil {
			callback.Finish()
		}

		// Verify image blob matches had expected digest.
		if got, want := id, imageDigest; got != want {
			return &unexpectedImageDigest{got, want}
		}

	} else {
		c.Logger.Logf("Skipping image blob upload (matching hash exists)")

		id = imageDigest

		if _, err := io.Copy(sifHeader, io.LimitReader(r, sifHeaderSize)); err != nil {
			return fmt.Errorf("error reading local SIF file header: %w", err)
		}
	}

	// Populate image configuration.
	ic, err := reg.processImageHeader(id, description, sifHeader.Bytes())
	if err != nil {
		return fmt.Errorf("process image failed: %w", err)
	}

	cs, cd, err := reg.uploadimageConfig(ctx, creds, name, ic)
	if err != nil {
		return fmt.Errorf("upload image config failed: %w", err)
	}

	md, err := reg.uploadImageManifest(ctx, creds, name, hash, cd, id, cs, size)
	if err != nil {
		return fmt.Errorf("upload image manifest failed: %w", err)
	}

	idx := v1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
	}

	idx.Manifests = append(idx.Manifests, v1.Descriptor{
		MediaType: v1.MediaTypeImageManifest,
		Digest:    md,
		Platform: &v1.Platform{
			Architecture: ic.Architecture,
			OS:           ic.OS,
		},
	})

	// Add tags
	for _, ref := range tags {
		c.Logger.Logf("Tag: %v", ref)

		if _, err := reg.uploadManifest(ctx, creds, name, ref, idx, v1.MediaTypeImageIndex); err != nil {
			return fmt.Errorf("error uploading index: %w", err)
		}
	}

	return nil
}

func (r *ociRegistry) existingImageBlob(ctx context.Context, creds credentials, name string, d digest.Digest) (bool, error) {
	u := r.baseURL.ResolveReference(&url.URL{Path: fmt.Sprintf("v2/%v/blobs/%v", name, d.String())})

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.String(), nil)
	if err != nil {
		return false, fmt.Errorf("error checking for existing layer: %w", err)
	}

	res, err := r.doRequest(req, creds)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	// TODO: should we validate 'Content-Length' here?
	return res.StatusCode == http.StatusOK && d.String() == res.Header.Get("Docker-Content-Digest"), nil
}

// uploadimageConfig uploads ic into namespace name of the registry, using credentials c.
//
// On success, the config size and digest are returned.
func (r *ociRegistry) uploadimageConfig(ctx context.Context, creds credentials, name string, ic imageConfig) (size int64, d digest.Digest, err error) {
	b, err := json.Marshal(ic)
	if err != nil {
		return 0, "", err
	}

	log.Logf("Starting image config upload: name=[%v], size=[%v]", name, len(b))
	defer func(t time.Time) {
		log.Logf("Finished image config upload: took=[%v] digest=[%v] err=[%v]", time.Since(t), d.String(), err)
	}(time.Now())

	d, _, err = r.uploadBlob(ctx, creds, name, int64(len(b)), bytes.NewReader(b))
	if err != nil {
		return 0, "", err
	}

	return int64(len(b)), d, err
}

// uploadImageManifest uploads an image manifest to the registry, naming it name:ref. The
// corresponding config blob has digest configDigest of size configSize. The corresponding image
// blob has digest imageDigest of size imageSize.
//
// On success, the manifest digest is returned.
func (r *ociRegistry) uploadImageManifest(ctx context.Context, creds credentials, name, ref string, configDigest, imageDigest digest.Digest, configSize, imageSize int64) (d digest.Digest, err error) {
	r.logger.Logf("Starting image manifest upload: name=[%v], ref=[%v]", name, ref)
	defer func(t time.Time) {
		r.logger.Logf("Finished image manifest upload: took=[%v] digest=[%v], err=[%v]", time.Since(t), d.String(), err)
	}(time.Now())

	m := v1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Config: v1.Descriptor{
			MediaType: mediaTypeSIFConfig,
			Digest:    configDigest,
			Size:      configSize,
		},
		Layers: []v1.Descriptor{
			{
				MediaType: mediaTypeSIFLayer,
				Digest:    imageDigest,
				Size:      imageSize,
			},
		},
	}
	return r.uploadV1Manifest(ctx, creds, name, ref, m)
}

func (r *ociRegistry) uploadImageBlob(ctx context.Context, creds credentials, name string, size int64, rd io.Reader) (digest.Digest, int64, error) {
	return r.uploadBlob(ctx, creds, name, size, rd)
}

const maxChunkSize int64 = 5 * 1024 * 1024

func (r *ociRegistry) uploadBlob(ctx context.Context, creds credentials, name string, size int64, rd io.Reader) (digest.Digest, int64, error) {
	u, creds, err := r.openUploadBlobSession(ctx, creds, name)
	if err != nil {
		return "", 0, err
	}

	// Accumulate digest as we upload chunks.
	h := digest.Canonical.Hash()
	tee := io.TeeReader(rd, h)

	var totalBytesUploaded int64

	// Send chunks.
	for offset := int64(0); offset < size; offset += maxChunkSize {
		chunkSize := maxChunkSize
		if offset+chunkSize > size {
			chunkSize = size - offset // last chunk
		}

		if u, err = r.uploadBlobPart(ctx, creds, u, tee, chunkSize, offset); err != nil {
			return "", 0, err
		}

		totalBytesUploaded += chunkSize
	}

	d := digest.NewDigest(digest.Canonical, h)

	if err := r.closeUploadBlobSession(ctx, creds, u, d); err != nil {
		return "", 0, err
	}

	return d, totalBytesUploaded, nil
}

func (r *ociRegistry) openUploadBlobSession(ctx context.Context, creds credentials, name string) (*url.URL, *bearerTokenCredentials, error) {
	u := &url.URL{Path: fmt.Sprintf("v2/%v/blobs/uploads/", name)}

	req, err := r.newRequest(ctx, http.MethodPost, u, nil)
	if err != nil {
		return nil, nil, err
	}

	res, err := r.doRequest(req, creds, withNamespaceAccess(name, accessTypePush))
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()

	if u, err = getRelativeLocation(res); err != nil {
		return nil, nil, err
	}

	// Strip prefix from Authorization header
	parts := strings.SplitN(req.Header.Get("Authorization"), " ", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("%w malformed Authorization header (%v)", errHTTP, req.Header.Get("Authorization"))
	}

	return u, &bearerTokenCredentials{authToken: parts[1]}, nil
}

// closeUploadBlobSession closes a blob upload session using relative URL u, including digest d.
func (r *ociRegistry) closeUploadBlobSession(ctx context.Context, creds credentials, u *url.URL, d digest.Digest) error {
	q := u.Query()
	q.Set("digest", d.String())
	u.RawQuery = q.Encode()

	req, err := r.newRequest(ctx, http.MethodPut, u, nil)
	if err != nil {
		return err
	}

	res, err := r.doRequestWithCredentials(req, creds)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return nil
}

// uploadBlobPart uploads a chunk of a blob read from rd using relative URL u. The chunk is located
// at offset and is of size chunkSize.
func (r *ociRegistry) uploadBlobPart(ctx context.Context, creds credentials, u *url.URL, rd io.Reader, chunkSize, offset int64) (*url.URL, error) {
	req, err := r.newRequest(ctx, http.MethodPatch, u, io.LimitReader(rd, chunkSize))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Range", fmt.Sprintf("%v-%v", offset, offset+chunkSize-1))
	req.Header.Set("Content-Length", strconv.FormatInt(chunkSize, 10))

	res, err := r.doRequestWithCredentials(req, creds)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	return getRelativeLocation(res)
}

// getRelativeLocation returns the relative URL contained in the `Location` header of res.
func getRelativeLocation(res *http.Response) (*url.URL, error) {
	u, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		return nil, err
	}

	u.Path = strings.TrimPrefix(u.Path, "/")

	return u, nil
}

func getSigned(f *sif.FileImage) (bool, error) {
	sigs, err := f.GetDescriptors(sif.WithDataType(sif.DataSignature))
	if err != nil {
		return false, err
	}
	return len(sigs) > 0, nil
}

// getEncrypted returns a boolean indicating whether the primary system partition in f is
// encrypted.
func getEncrypted(f *sif.FileImage) (bool, error) {
	od, err := f.GetDescriptor(sif.WithPartitionType(sif.PartPrimSys))
	if err != nil {
		return false, err
	}

	t, _, _, err := od.PartitionMetadata()
	if err != nil {
		return false, err
	}

	return (t == sif.FsEncryptedSquashfs), nil
}

// processImageHeader creates an imageConfig using the supplied hash, description, and SIF header
// contained in b.
func (r *ociRegistry) processImageHeader(rootFS digest.Digest, description string, b []byte) (imageConfig, error) {
	f, err := sif.LoadContainer(sif.NewBuffer(b))
	if err != nil {
		return imageConfig{}, err
	}
	defer func() {
		if err := f.UnloadContainer(); err != nil {
			r.logger.Logf("Failed to unload container: %v", err)
		}
	}()

	signed, err := getSigned(f)
	if err != nil {
		return imageConfig{}, err
	}

	encrypted, err := getEncrypted(f)
	if err != nil {
		return imageConfig{}, err
	}

	ic := imageConfig{
		Architecture: f.PrimaryArch(),
		OS:           "linux",
		RootFS:       rootFS,
		Description:  description,
		Signed:       signed,
		Encrypted:    encrypted,
	}

	return ic, nil
}

// manifestURL returns the relative URL associated with name/ref.
func manifestURL(name, ref string) *url.URL {
	return &url.URL{
		Path: fmt.Sprintf("v2/%v/manifests/%v", name, ref),
	}
}

// uploadManifest uploads manifest v of type contentType to the registry, and associates it with
// name/ref. If ref is empty, the manifest digest is used.
func (r *ociRegistry) uploadManifest(ctx context.Context, creds credentials, name, ref string, v interface{}, contentType string) (digest.Digest, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}

	d := digest.FromBytes(b)

	if ref == "" {
		ref = d.String()
	}

	req, err := r.newRequest(ctx, http.MethodPut, manifestURL(name, ref), bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)

	res, err := r.doRequest(req, creds, withNamespaceAccess(name, accessTypePush))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	return d, nil
}

// UploadV1Index uploads image index idx to the registry, and associates it with name/ref. If ref
// is empty, the image index digest is used.
func (r *ociRegistry) UploadV1Index(ctx context.Context, creds credentials, name, ref string, idx v1.Index) (digest.Digest, error) {
	return r.uploadManifest(ctx, creds, name, ref, idx, v1.MediaTypeImageIndex)
}

// uploadV1Manifest uploads manifest m to the registry, and associates it with name/ref. If ref is
// empty, the manifest digest is used.
func (r *ociRegistry) uploadV1Manifest(ctx context.Context, creds credentials, name, ref string, m v1.Manifest) (digest.Digest, error) {
	return r.uploadManifest(ctx, creds, name, ref, m, v1.MediaTypeImageManifest)
}
