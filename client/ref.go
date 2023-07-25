// Copyright (c) 2018-2022, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"net/url"
	"strings"
)

// Scheme is the required scheme for Library URIs.
const Scheme = "library"

// A Ref represents a parsed Library URI.
//
// The general form represented is:
//
//	scheme:[//host][/]path[:tags]
//
// The host contains both the hostname and port, if present. These values can be accessed using
// the Hostname and Port methods.
//
// Examples of valid URIs:
//
//	library:path:tags
//	library:/path:tags
//	library:///path:tags
//	library://host/path:tags
//	library://host:port/path:tags
//
// The tags component is a comma-separated list of one or more tags.
type Ref struct {
	Host string   // host or host:port
	Path string   // project or entity/project
	Tags []string // list of tags
}

// parseTags takes raw tags and returns a slice of tags.
func parseTags(rawTags string) (tags []string, err error) {
	if len(rawTags) == 0 {
		return nil, ErrRefTagsNotValid
	}

	return strings.Split(rawTags, ","), nil
}

// parsePath takes the URI path and parses the path and tags.
func parsePath(rawPath string) (path string, tags []string, err error) {
	if len(rawPath) == 0 {
		return "", nil, ErrRefPathNotValid
	}

	// The path is separated from the tags (if present) by a single colon.
	parts := strings.Split(rawPath, ":")
	if len(parts) > 2 {
		return "", nil, ErrRefPathNotValid
	}

	// TODO: not sure we should modify the path here...
	// Name can optionally start with a leading "/".
	path = strings.TrimPrefix(parts[0], "/")
	if len(path) == 0 {
		return "", nil, ErrRefPathNotValid
	}

	if len(parts) > 1 {
		tags, err = parseTags(parts[1])
		if err != nil {
			return "", nil, err
		}
	} else {
		tags = nil
	}
	return path, tags, nil
}

// parse parses a raw Library reference, optionally taking into account ambiguity that exists
// within Singularity Library references.
func parse(rawRef string, ambiguous bool) (*Ref, error) {
	var u *url.URL

	if ambiguous && strings.HasPrefix(rawRef, "library://") && !strings.HasPrefix(rawRef, "library:///") {
		// Parse as if there's no host component.
		uri, err := url.Parse(strings.Replace(rawRef, "library://", "library:///", 1))
		if err != nil {
			return nil, err
		}

		// If the path contains one or three parts, there was no host component. Otherwise, fall
		// through to the normal logic.
		if n := len(strings.Split(uri.Path[1:], "/")); n == 1 || n == 3 {
			u = uri
		}
	}

	if u == nil {
		var err error
		if u, err = url.Parse(rawRef); err != nil {
			return nil, err
		}
	}

	if u.Scheme != Scheme {
		return nil, ErrRefSchemeNotValid
	}
	if u.User != nil {
		return nil, ErrRefUserNotPermitted
	}
	if u.RawQuery != "" {
		return nil, ErrRefQueryNotPermitted
	}
	if u.Fragment != "" {
		return nil, ErrRefFragmentNotPermitted
	}

	rawPath := u.Path
	if u.Opaque != "" {
		rawPath = u.Opaque
	}

	path, tags, err := parsePath(rawPath)
	if err != nil {
		return nil, err
	}

	r := &Ref{
		Host: u.Host,
		Path: path,
		Tags: tags,
	}
	return r, nil
}

// Parse parses a raw Library reference.
func Parse(rawRef string) (*Ref, error) {
	return parse(rawRef, false)
}

// ParseAmbiguous behaves like Parse, but takes into account ambiguity that exists within
// Singularity Library references that begin with the prefix "library://".
//
// In particular, Singularity supports hostless Library references in the form of "library://path".
// This creates ambiguity in whether or not a host is present in the path or not. To account for
// this, ParseAmbiguous treats library references beginning with "library://" followed by one or
// three path components (ex. "library://a", "library://a/b/c") as hostless. All other references
// are treated the same as Parse.
func ParseAmbiguous(rawRef string) (*Ref, error) {
	return parse(rawRef, true)
}

// String reassembles the ref into a valid URI string. The general form of the result is one of:
//
//	scheme:path[:tags]
//	scheme://host/path[:tags]
//
// If u.Host is empty, String uses the first form; otherwise it uses the second form.
func (r *Ref) String() string {
	u := url.URL{
		Scheme: Scheme,
		Host:   r.Host,
	}

	rawPath := r.Path
	if len(r.Tags) > 0 {
		rawPath += ":" + strings.Join(r.Tags, ",")
	}

	if u.Host != "" {
		u.Path = rawPath
	} else {
		u.Opaque = rawPath
	}

	return u.String()
}

// Hostname returns r.Host, without any port number.
//
// If Host is an IPv6 literal with a port number, Hostname returns the IPv6 literal without the
// square brackets. IPv6 literals may include a zone identifier.
func (r *Ref) Hostname() string {
	colon := strings.IndexByte(r.Host, ':')
	if colon == -1 {
		return r.Host
	}
	if i := strings.IndexByte(r.Host, ']'); i != -1 {
		return strings.TrimPrefix(r.Host[:i], "[")
	}
	return r.Host[:colon]
}

// Port returns the port part of u.Host, without the leading colon. If u.Host doesn't contain a
// port, Port returns an empty string.
func (r *Ref) Port() string {
	colon := strings.IndexByte(r.Host, ':')
	if colon == -1 {
		return ""
	}
	if i := strings.Index(r.Host, "]:"); i != -1 {
		return r.Host[i+len("]:"):]
	}
	if strings.Contains(r.Host, "]") {
		return ""
	}
	return r.Host[colon+len(":"):]
}
