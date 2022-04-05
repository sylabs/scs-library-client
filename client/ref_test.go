// Copyright (c) 2018-2022, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name            string
		rawRef          string
		wantErr         bool
		wantErrSpecific error
		wantHost        string
		wantPath        string
		wantTags        []string
	}{
		// The URI must be valid.
		{"InvalidURL", ":", true, nil, "", "", nil},

		// A path is required.
		{"NoName", "library:", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndTag", "library::tag", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndSlash", "library:/", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndSlashAndTag", "library:/:tag", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndSlashes", "library:///", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndSlashesAndTag", "library:///:tag", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHost", "library://host", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHostSlash", "library://host/", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHostSlashAndTag", "library://host/:tag", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHostPort", "library://host:443", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHostPortSlash", "library://host:443/", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHostPortSlashAndTag", "library://host:443/:tag", true, ErrRefPathNotValid, "", "", nil},

		// The scheme must be present.
		{"NoScheme", "", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeSlash", "/", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeSlashAndTag", "/:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeSlashes", "///", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeSlashesAndTag", "///:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeAndHost", "//host/", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeAndHostAndTag", "//host/:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeAndHostPort", "//host:443/", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeAndHostPortAndTag", "//host:443/:tag", true, ErrRefSchemeNotValid, "", "", nil},

		// The scheme must be valid.
		{"BadScheme", "bad:project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndTag", "bad:project:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndSlash", "bad:/project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndSlashAndTag", "bad:/project:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndSlashes", "bad:///project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndSlashesAndTag", "bad:///project:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndHost", "bad://host/project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndHostAndTag", "bad://host/project:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndHostPort", "bad://host:443/project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndHostPortAndTag", "bad://host:443/project:tag", true, ErrRefSchemeNotValid, "", "", nil},

		// User not permitted.
		{"UserAndHost", "library://user@host/project", true, ErrRefUserNotPermitted, "", "", nil},
		{"UserAndHostAndTag", "library://user@host/project:tag", true, ErrRefUserNotPermitted, "", "", nil},
		{"UserAndHostPort", "library://user@host:443/project", true, ErrRefUserNotPermitted, "", "", nil},
		{"UserAndHostPortAndTag", "library://user@host:443/project:tag", true, ErrRefUserNotPermitted, "", "", nil},

		// Query not permitted.
		{"Query", "library:project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndTag", "library:project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndSlash", "library:/project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndSlashAndTag", "library:/project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndSlashes", "library:///project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndSlashesAndTag", "library:///project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndHost", "library://host/project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndHostAndTag", "library://host/project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndHostPort", "library://host:443/project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndHostPortAndTag", "library://host:443/project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},

		// Fragment not permitted.
		{"Fragment", "library:project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndTag", "library:project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndSlash", "library:/project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndSlashAndTag", "library:/project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndSlashes", "library:///project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndSlashesAndTag", "library:///project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndHost", "library://host/project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndHostAndTag", "library://host/project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndHostPort", "library://host:443/project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndHostPortAndTag", "library://host:443/project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},

		// The URI cannot have a trailing colon without tags.
		{"TrailingSemicolon", "library:project:", true, ErrRefTagsNotValid, "", "", nil},
		{"TrailingSemicolonAndSlash", "library:/project:", true, ErrRefTagsNotValid, "", "", nil},
		{"TrailingSemicolonAndSlashes", "library:///project:", true, ErrRefTagsNotValid, "", "", nil},
		{"TrailingSemicolonAndHost", "library://host/project:", true, ErrRefTagsNotValid, "", "", nil},
		{"TrailingSemicolonAndHostPort", "library://host:443/project:", true, ErrRefTagsNotValid, "", "", nil},

		// The URI path can only have one colon to separate path/tags.
		{"ExtraSemicolon", "library:project:tag:extra", true, ErrRefPathNotValid, "", "", nil},
		{"ExtraSemicolonAndSlash", "library:/project:tag:extra", true, ErrRefPathNotValid, "", "", nil},
		{"ExtraSemicolonAndSlashes", "library:///project:tag:extra", true, ErrRefPathNotValid, "", "", nil},
		{"ExtraSemicolonAndHost", "library://host/project:tag:extra", true, ErrRefPathNotValid, "", "", nil},
		{"ExtraSemicolonAhdHostPort", "library://host:443/project:tag:extra", true, ErrRefPathNotValid, "", "", nil},

		// Test valid abbreviated paths (project).
		{"AbbreviatedPath", "library:project", false, nil, "", "project", nil},
		{"AbbreviatedPathAndTag", "library:project:tag", false, nil, "", "project", []string{"tag"}},
		{"AbbreviatedPathAndSlash", "library:/project", false, nil, "", "project", nil},
		{"AbbreviatedPathAndSlashAndTag", "library:/project:tag", false, nil, "", "project", []string{"tag"}},
		{"AbbreviatedPathAndSlashes", "library:///project", false, nil, "", "project", nil},
		{"AbbreviatedPathAndSlashesAndTag", "library:///project:tag", false, nil, "", "project", []string{"tag"}},
		{"AbbreviatedPathAndHost", "library://host/project", false, nil, "host", "project", nil},
		{"AbbreviatedPathAndHostAndTag", "library://host/project:tag", false, nil, "host", "project", []string{"tag"}},
		{"AbbreviatedPathAndHostPort", "library://host:443/project", false, nil, "host:443", "project", nil},
		{"AbbreviatedPathAndHostPortAndTag", "library://host:443/project:tag", false, nil, "host:443", "project", []string{"tag"}},

		// Test valid paths (entity/collection/container).
		{"LegacyPath", "library:entity/collection/container", false, nil, "", "entity/collection/container", nil},
		{"LegacyPathAndTag", "library:entity/collection/container:tag", false, nil, "", "entity/collection/container", []string{"tag"}},
		{"LegacyPathAndSlash", "library:/entity/collection/container", false, nil, "", "entity/collection/container", nil},
		{"LegacyPathAndSlashAndTag", "library:/entity/collection/container:tag", false, nil, "", "entity/collection/container", []string{"tag"}},
		{"LegacyPathAndSlashes", "library:///entity/collection/container", false, nil, "", "entity/collection/container", nil},
		{"LegacyPathAndSlashesAndTag", "library:///entity/collection/container:tag", false, nil, "", "entity/collection/container", []string{"tag"}},
		{"LegacyPathAndHost", "library://host/entity/collection/container", false, nil, "host", "entity/collection/container", nil},
		{"LegacyPathAndHostAndTag", "library://host/entity/collection/container:tag", false, nil, "host", "entity/collection/container", []string{"tag"}},
		{"LegacyPathAndHostPort", "library://host:443/entity/collection/container", false, nil, "host:443", "entity/collection/container", nil},
		{"LegacyPathAndHostPortAndTag", "library://host:443/entity/collection/container:tag", false, nil, "host:443", "entity/collection/container", []string{"tag"}},

		// Test with a different number of tags.
		{"TagsNone", "library:project", false, nil, "", "project", nil},
		{"TagsOne", "library:project:tag1", false, nil, "", "project", []string{"tag1"}},
		{"TagsTwo", "library:project:tag1,tag2", false, nil, "", "project", []string{"tag1", "tag2"}},
		{"TagsThree", "library:project:tag1,tag2,tag3", false, nil, "", "project", []string{"tag1", "tag2", "tag3"}},

		// Test with IP addresses.
		{"IPv4Host", "library://127.0.0.1/project", false, nil, "127.0.0.1", "project", nil},
		{"IPv4HostPort", "library://127.0.0.1:443/project", false, nil, "127.0.0.1:443", "project", nil},
		{"IPv6Host", "library://[fe80::1ff:fe23:4567:890a]/project", false, nil, "[fe80::1ff:fe23:4567:890a]", "project", nil},
		{"IPv6HostPort", "library://[fe80::1ff:fe23:4567:890a]:443/project", false, nil, "[fe80::1ff:fe23:4567:890a]:443", "project", nil},
		{"IPv6HostZone", "library://[fe80::1ff:fe23:4567:890a%25eth1]/project", false, nil, "[fe80::1ff:fe23:4567:890a%eth1]", "project", nil},
		{"IPv6HostZonePort", "library://[fe80::1ff:fe23:4567:890a%25eth1]:443/project", false, nil, "[fe80::1ff:fe23:4567:890a%eth1]:443", "project", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Parse(tt.rawRef)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
			if tt.wantErrSpecific != nil {
				if got, want := err, tt.wantErrSpecific; got != want {
					t.Fatalf("got err %v, want %v", got, want)
				}
			}

			if err == nil {
				if got, want := r.Host, tt.wantHost; got != want {
					t.Errorf("got host %v, want %v", got, want)
				}
				if got, want := r.Path, tt.wantPath; got != want {
					t.Errorf("got path %v, want %v", got, want)
				}
				if got, want := r.Tags, tt.wantTags; !reflect.DeepEqual(got, want) {
					t.Errorf("got tags %v, want %v", got, want)
				}
			}
		})
	}
}

func TestParseAmbiguous(t *testing.T) {
	tests := []struct {
		name            string
		rawRef          string
		wantErr         bool
		wantErrSpecific error
		wantHost        string
		wantPath        string
		wantTags        []string
	}{
		// The URI must be valid.
		{"InvalidURL", ":", true, nil, "", "", nil},

		// A path is required.
		{"NoName", "library:", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndTag", "library::tag", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndSlash", "library:/", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndSlashAndTag", "library:/:tag", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndSlashes", "library:///", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndSlashesAndTag", "library:///:tag", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHostSlash", "library://host/", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHostSlashAndTag", "library://host/:tag", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHostPortSlash", "library://host:443/", true, ErrRefPathNotValid, "", "", nil},
		{"NoNameAndHostPortSlashAndTag", "library://host:443/:tag", true, ErrRefPathNotValid, "", "", nil},

		// The scheme must be present.
		{"NoScheme", "", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeSlash", "/", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeSlashAndTag", "/:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeSlashes", "///", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeSlashesAndTag", "///:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeAndHost", "//host/", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeAndHostAndTag", "//host/:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeAndHostPort", "//host:443/", true, ErrRefSchemeNotValid, "", "", nil},
		{"NoSchemeAndHostPortAndTag", "//host:443/:tag", true, ErrRefSchemeNotValid, "", "", nil},

		// The scheme must be valid.
		{"BadScheme", "bad:project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndTag", "bad:project:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndSlash", "bad:/project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndSlashAndTag", "bad:/project:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndSlashes", "bad:///project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndSlashesAndTag", "bad:///project:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndHost", "bad://host/project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndHostAndTag", "bad://host/project:tag", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndHostPort", "bad://host:443/project", true, ErrRefSchemeNotValid, "", "", nil},
		{"BadSchemeAndHostPortAndTag", "bad://host:443/project:tag", true, ErrRefSchemeNotValid, "", "", nil},

		// User not permitted.
		{"UserAndHost", "library://user@host/project", true, ErrRefUserNotPermitted, "", "", nil},
		{"UserAndHostAndTag", "library://user@host/project:tag", true, ErrRefUserNotPermitted, "", "", nil},
		{"UserAndHostPort", "library://user@host:443/project", true, ErrRefUserNotPermitted, "", "", nil},
		{"UserAndHostPortAndTag", "library://user@host:443/project:tag", true, ErrRefUserNotPermitted, "", "", nil},

		// Query not permitted.
		{"Query", "library:project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndTag", "library:project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndSlash", "library:/project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndSlashAndTag", "library:/project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndSlashes", "library:///project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndSlashesAndTag", "library:///project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndHost", "library://host/project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndHostAndTag", "library://host/project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndHostPort", "library://host:443/project?query", true, ErrRefQueryNotPermitted, "", "", nil},
		{"QueryAndHostPortAndTag", "library://host:443/project:tag?query", true, ErrRefQueryNotPermitted, "", "", nil},

		// Fragment not permitted.
		{"Fragment", "library:project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndTag", "library:project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndSlash", "library:/project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndSlashAndTag", "library:/project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndSlashes", "library:///project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndSlashesAndTag", "library:///project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndHost", "library://host/project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndHostAndTag", "library://host/project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndHostPort", "library://host:443/project#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},
		{"FragmentAndHostPortAndTag", "library://host:443/project:tag#fragment", true, ErrRefFragmentNotPermitted, "", "", nil},

		// The URI cannot have a trailing colon without tags.
		{"TrailingSemicolon", "library:project:", true, ErrRefTagsNotValid, "", "", nil},
		{"TrailingSemicolonAndSlash", "library:/project:", true, ErrRefTagsNotValid, "", "", nil},
		{"TrailingSemicolonAndSlashes", "library:///project:", true, ErrRefTagsNotValid, "", "", nil},
		{"TrailingSemicolonAndHost", "library://host/project:", true, ErrRefTagsNotValid, "", "", nil},
		{"TrailingSemicolonAndHostPort", "library://host:443/project:", true, ErrRefTagsNotValid, "", "", nil},

		// The URI path can only have one colon to separate path/tags.
		{"ExtraSemicolon", "library:project:tag:extra", true, ErrRefPathNotValid, "", "", nil},
		{"ExtraSemicolonAndSlash", "library:/project:tag:extra", true, ErrRefPathNotValid, "", "", nil},
		{"ExtraSemicolonAndSlashes", "library:///project:tag:extra", true, ErrRefPathNotValid, "", "", nil},
		{"ExtraSemicolonAndHost", "library://host/project:tag:extra", true, ErrRefPathNotValid, "", "", nil},
		{"ExtraSemicolonAhdHostPort", "library://host:443/project:tag:extra", true, ErrRefPathNotValid, "", "", nil},

		// Test valid abbreviated paths (project).
		{"AbbreviatedPath", "library:project", false, nil, "", "project", nil},
		{"AbbreviatedPathAndTag", "library:project:tag", false, nil, "", "project", []string{"tag"}},
		{"AbbreviatedPathAndSlash", "library:/project", false, nil, "", "project", nil},
		{"AbbreviatedPathAndSlashAndTag", "library:/project:tag", false, nil, "", "project", []string{"tag"}},
		{"AbbreviatedPathAndSlashes", "library:///project", false, nil, "", "project", nil},
		{"AbbreviatedPathAndSlashesAndTag", "library:///project:tag", false, nil, "", "project", []string{"tag"}},
		{"AbbreviatedPathHostless", "library://project", false, nil, "", "project", nil},
		{"AbbreviatedPathHostlessAndTag", "library://project:tag", false, nil, "", "project", []string{"tag"}},
		{"AbbreviatedPathAndHost", "library://host/project", false, nil, "host", "project", nil},
		{"AbbreviatedPathAndHostAndTag", "library://host/project:tag", false, nil, "host", "project", []string{"tag"}},
		{"AbbreviatedPathAndHostPort", "library://host:443/project", false, nil, "host:443", "project", nil},
		{"AbbreviatedPathAndHostPortAndTag", "library://host:443/project:tag", false, nil, "host:443", "project", []string{"tag"}},

		// Test valid paths (entity/collection/container).
		{"LegacyPath", "library:entity/collection/container", false, nil, "", "entity/collection/container", nil},
		{"LegacyPathAndTag", "library:entity/collection/container:tag", false, nil, "", "entity/collection/container", []string{"tag"}},
		{"LegacyPathAndSlash", "library:/entity/collection/container", false, nil, "", "entity/collection/container", nil},
		{"LegacyPathAndSlashAndTag", "library:/entity/collection/container:tag", false, nil, "", "entity/collection/container", []string{"tag"}},
		{"LegacyPathAndSlashes", "library:///entity/collection/container", false, nil, "", "entity/collection/container", nil},
		{"LegacyPathAndSlashesAndTag", "library:///entity/collection/container:tag", false, nil, "", "entity/collection/container", []string{"tag"}},
		{"LegacyPathHostless", "library://entity/collection/container", false, nil, "", "entity/collection/container", nil},
		{"LegacyPathHostlessAndTag", "library://entity/collection/container:tag", false, nil, "", "entity/collection/container", []string{"tag"}},
		{"LegacyPathAndHost", "library://host/entity/collection/container", false, nil, "host", "entity/collection/container", nil},
		{"LegacyPathAndHostAndTag", "library://host/entity/collection/container:tag", false, nil, "host", "entity/collection/container", []string{"tag"}},
		{"LegacyPathAndHostPort", "library://host:443/entity/collection/container", false, nil, "host:443", "entity/collection/container", nil},
		{"LegacyPathAndHostPortAndTag", "library://host:443/entity/collection/container:tag", false, nil, "host:443", "entity/collection/container", []string{"tag"}},

		// Test with a different number of tags.
		{"TagsNone", "library:project", false, nil, "", "project", nil},
		{"TagsOne", "library:project:tag1", false, nil, "", "project", []string{"tag1"}},
		{"TagsTwo", "library:project:tag1,tag2", false, nil, "", "project", []string{"tag1", "tag2"}},
		{"TagsThree", "library:project:tag1,tag2,tag3", false, nil, "", "project", []string{"tag1", "tag2", "tag3"}},

		// Test with IP addresses.
		{"IPv4Host", "library://127.0.0.1/project", false, nil, "127.0.0.1", "project", nil},
		{"IPv4HostPort", "library://127.0.0.1:443/project", false, nil, "127.0.0.1:443", "project", nil},
		{"IPv6Host", "library://[fe80::1ff:fe23:4567:890a]/project", false, nil, "[fe80::1ff:fe23:4567:890a]", "project", nil},
		{"IPv6HostPort", "library://[fe80::1ff:fe23:4567:890a]:443/project", false, nil, "[fe80::1ff:fe23:4567:890a]:443", "project", nil},
		{"IPv6HostZone", "library://[fe80::1ff:fe23:4567:890a%25eth1]/project", false, nil, "[fe80::1ff:fe23:4567:890a%eth1]", "project", nil},
		{"IPv6HostZonePort", "library://[fe80::1ff:fe23:4567:890a%25eth1]:443/project", false, nil, "[fe80::1ff:fe23:4567:890a%eth1]:443", "project", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := ParseAmbiguous(tt.rawRef)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
			if tt.wantErrSpecific != nil {
				if got, want := err, tt.wantErrSpecific; got != want {
					t.Fatalf("got err %v, want %v", got, want)
				}
			}

			if err == nil {
				if got, want := r.Host, tt.wantHost; got != want {
					t.Errorf("got host %v, want %v", got, want)
				}
				if got, want := r.Path, tt.wantPath; got != want {
					t.Errorf("got path %v, want %v", got, want)
				}
				if got, want := r.Tags, tt.wantTags; !reflect.DeepEqual(got, want) {
					t.Errorf("got tags %v, want %v", got, want)
				}
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		path       string
		tags       []string
		wantString string
	}{
		// Test valid abbreviated paths (project).
		{"AbbreviatedPath", "", "project", nil, "library:project"},
		{"AbbreviatedPathAndTag", "", "project", []string{"tag"}, "library:project:tag"},
		{"AbbreviatedPathAndSlash", "", "project", nil, "library:project"},
		{"AbbreviatedPathAndSlashAndTag", "", "project", []string{"tag"}, "library:project:tag"},
		{"AbbreviatedPathAndHost", "host", "project", nil, "library://host/project"},
		{"AbbreviatedPathAndHostAndTag", "host", "project", []string{"tag"}, "library://host/project:tag"},
		{"AbbreviatedPathAndHostPort", "host:443", "project", nil, "library://host:443/project"},
		{"AbbreviatedPathAndHostPortAndTag", "host:443", "project", []string{"tag"}, "library://host:443/project:tag"},

		// Test valid paths (entity/collection/container).
		{"LegacyPath", "", "entity/collection/container", nil, "library:entity/collection/container"},
		{"LegacyPathAndTag", "", "entity/collection/container", []string{"tag"}, "library:entity/collection/container:tag"},
		{"LegacyPathAndSlash", "", "entity/collection/container", nil, "library:entity/collection/container"},
		{"LegacyPathAndSlashAndTag", "", "entity/collection/container", []string{"tag"}, "library:entity/collection/container:tag"},
		{"LegacyPathAndHost", "host", "entity/collection/container", nil, "library://host/entity/collection/container"},
		{"LegacyPathAndHostAndTag", "host", "entity/collection/container", []string{"tag"}, "library://host/entity/collection/container:tag"},
		{"LegacyPathAndHostPort", "host:443", "entity/collection/container", nil, "library://host:443/entity/collection/container"},
		{"LegacyPathAndHostPortAndTag", "host:443", "entity/collection/container", []string{"tag"}, "library://host:443/entity/collection/container:tag"},

		// Test with a different number of tags.
		{"TagsNone", "", "project", nil, "library:project"},
		{"TagsOne", "", "project", []string{"tag1"}, "library:project:tag1"},
		{"TagsTwo", "", "project", []string{"tag1", "tag2"}, "library:project:tag1,tag2"},
		{"TagsThree", "", "project", []string{"tag1", "tag2", "tag3"}, "library:project:tag1,tag2,tag3"},

		// Test with IP addresses.
		{"IPv4Host", "127.0.0.1", "project", nil, "library://127.0.0.1/project"},
		{"IPv4HostPort", "127.0.0.1:443", "project", nil, "library://127.0.0.1:443/project"},
		{"IPv6Host", "[fe80::1ff:fe23:4567:890a]", "project", nil, "library://[fe80::1ff:fe23:4567:890a]/project"},
		{"IPv6HostPort", "[fe80::1ff:fe23:4567:890a]:443", "project", nil, "library://[fe80::1ff:fe23:4567:890a]:443/project"},
		{"IPv6HostZone", "[fe80::1ff:fe23:4567:890a%eth1]", "project", nil, "library://[fe80::1ff:fe23:4567:890a%25eth1]/project"},
		{"IPv6HostZonePort", "[fe80::1ff:fe23:4567:890a%eth1]:443", "project", nil, "library://[fe80::1ff:fe23:4567:890a%25eth1]:443/project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Ref{
				Host: tt.host,
				Path: tt.path,
				Tags: tt.tags,
			}
			if got, want := r.String(), tt.wantString; got != want {
				t.Errorf("got string %v, want %v", got, want)
			}
		})
	}
}

func TestHostnamePort(t *testing.T) {
	tests := []struct {
		name         string
		rawRef       string
		wantHostname string
		wantPort     string
	}{
		// Test without hostname/port.
		{"Path", "library:project", "", ""},
		{"PathAndSlash", "library:/project", "", ""},
		{"PathAndSlashes", "library:///project", "", ""},

		// Test with hostnames, with/without port.
		{"IPv4Host", "library://host/project", "host", ""},
		{"IPv4HostPort", "library://host:443/project", "host", "443"},
		{"IPv4Host", "library://host.name/project", "host.name", ""},
		{"IPv4HostPort", "library://host.name:443/project", "host.name", "443"},

		// Test with IP addresses, with/without port.
		{"IPv4Host", "library://127.0.0.1/project", "127.0.0.1", ""},
		{"IPv4HostPort", "library://127.0.0.1:443/project", "127.0.0.1", "443"},
		{"IPv6Host", "library://[fe80::1ff:fe23:4567:890a]/project", "fe80::1ff:fe23:4567:890a", ""},
		{"IPv6HostPort", "library://[fe80::1ff:fe23:4567:890a]:443/project", "fe80::1ff:fe23:4567:890a", "443"},
		{"IPv6HostZone", "library://[fe80::1ff:fe23:4567:890a%25eth1]/project", "fe80::1ff:fe23:4567:890a%eth1", ""},
		{"IPv6HostZonePort", "library://[fe80::1ff:fe23:4567:890a%25eth1]:443/project", "fe80::1ff:fe23:4567:890a%eth1", "443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Parse(tt.rawRef)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			if got, want := r.Hostname(), tt.wantHostname; got != want {
				t.Errorf("got hostname %v, want %v", got, want)
			}
			if got, want := r.Port(), tt.wantPort; got != want {
				t.Errorf("got port %v, want %v", got, want)
			}
		})
	}
}
