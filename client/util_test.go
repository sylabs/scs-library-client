// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"reflect"
	"testing"

	"github.com/globalsign/mgo/bson"
)

func Test_isLibraryPullRef(t *testing.T) {
	tests := []struct {
		name       string
		libraryRef string
		want       bool
	}{
		{"Good long ref 1", "library://entity/collection/image:tag", true},
		{"Good long ref 2", "entity/collection/image:tag", true},
		{"Good long ref 3", "entity/collection/image", true},
		{"Good short ref 1", "library://image:tag", true},
		{"Good short ref 2", "library://image", true},
		{"Good short ref 3", "library://collection/image:tag", true},
		{"Good short ref 4", "library://image", true},
		{"Good long sha ref 1", "library://entity/collection/image:sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", true},
		{"Good long sha ref 2", "entity/collection/image:sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", true},
		{"Good short sha ref 1", "library://image:sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", true},
		{"Good short sha ref 2", "image:sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", true},
		{"Good short sha ref 3", "library://collection/image:sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", true},
		{"Good short sha ref 4", "collection/image:sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", true},
		{"Too many components", "library://entity/collection/extra/image:tag", false},
		{"Bad character", "library://entity/collection/im,age:tag", false},
		{"Bad initial character", "library://entity/collection/-image:tag", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLibraryPullRef(tt.libraryRef); got != tt.want {
				t.Errorf("isLibraryPullRef() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isLibraryPushRef(t *testing.T) {
	tests := []struct {
		name       string
		libraryRef string
		want       bool
	}{
		{"Good long ref 1", "library://entity/collection/image:tag", true},
		{"Good long ref 2", "entity/collection/image:tag", true},
		{"Good long ref 3", "entity/collection/image", true},
		{"Short ref not allowed 1", "library://image:tag", false},
		{"Short ref not allowed 2", "library://image", false},
		{"Short ref not allowed 3", "library://collection/image:tag", false},
		{"Short ref not allowed 4", "library://image", false},
		{"Good long sha ref 1", "library://entity/collection/image:sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", true},
		{"Good long sha ref 2", "entity/collection/image:sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", true},
		{"Too many components", "library://entity/collection/extra/image:tag", false},
		{"Bad character", "library://entity/collection/im,age:tag", false},
		{"Bad initial character", "library://entity/collection/-image:tag", false},
		{"No capitals", "library://Entity/collection/image:tag", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLibraryPushRef(tt.libraryRef); got != tt.want {
				t.Errorf("isLibraryPushRef() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_IsRefPart(t *testing.T) {
	tests := []struct {
		name       string
		libraryRef string
		want       bool
	}{
		{"Good ref 1", "abc123", true},
		{"Good ref 2", "abc-123", true},
		{"Good ref 3", "abc_123", true},
		{"Good ref 4", "abc.123", true},
		{"Bad character", "abc,123", false},
		{"Bad initial character", "-abc123", false},
		{"No capitals", "Abc123", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRefPart(tt.libraryRef); got != tt.want {
				t.Errorf("IsRefPart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_IsImageHash(t *testing.T) {
	tests := []struct {
		name       string
		libraryRef string
		want       bool
	}{
		{"Good sha256", "sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", true},
		{"Good sif", "sif.5574b72c-7705-49cc-874e-424fc3b78116", true},
		{"sha256 too long", "sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88a", false},
		{"sha256 too short", "sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e8", false},
		{"sha256 bad character", "sha256.g50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88", false},
		{"sif too long", "sif.5574b72c-7705-49cc-874e-424fc3b78116a", false},
		{"sif too short", "sif.5574b72c-7705-49cc-874e-424fc3b7811", false},
		{"sif bad character", "sif.g574b72c-7705-49cc-874e-424fc3b78116", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsImageHash(tt.libraryRef); got != tt.want {
				t.Errorf("IsImageHash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_ParseLibraryPath(t *testing.T) {
	tests := []struct {
		name       string
		libraryRef string
		wantEnt    string
		wantCol    string
		wantCon    string
		wantTags   []string
	}{
		{"Good long ref 1", "library://entity/collection/image:tag", "entity", "collection", "image", []string{"tag"}},
		{"Good long ref 2", "entity/collection/image:tag", "entity", "collection", "image", []string{"tag"}},
		{"Good long ref multi tag", "library://entity/collection/image:tag1,tag2,tag3", "entity", "collection", "image", []string{"tag1", "tag2", "tag3"}},
		{"Good short ref 1", "library://image:tag", "", "", "image", []string{"tag"}},
		{"Good short ref 2", "image:tag", "", "", "image", []string{"tag"}},
		{"Good short ref 3", "library://collection/image:tag", "", "collection", "image", []string{"tag"}},
		{"Good short ref 4", "collection/image:tag", "", "collection", "image", []string{"tag"}},
		{"Good short ref multi tag", "library://image:tag1,tag2,tag3", "", "", "image", []string{"tag1", "tag2", "tag3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ent, col, con, tags := ParseLibraryPath(tt.libraryRef)
			if ent != tt.wantEnt {
				t.Errorf("ParseLibraryRef() = entity %v, want %v", ent, tt.wantEnt)
			}
			if col != tt.wantCol {
				t.Errorf("ParseLibraryRef() = collection %v, want %v", col, tt.wantCol)
			}
			if con != tt.wantCon {
				t.Errorf("ParseLibraryRef() = container %v, want %v", con, tt.wantCon)
			}
			if !reflect.DeepEqual(tags, tt.wantTags) {
				t.Errorf("ParseLibraryRef() = entity %v, want %v", tags, tt.wantTags)
			}
		})
	}
}

func TestIdInSlice(t *testing.T) {

	trueID := bson.NewObjectId().Hex()

	slice := []string{trueID, bson.NewObjectId().Hex(), bson.NewObjectId().Hex(), bson.NewObjectId().Hex()}
	if !IDInSlice(trueID, slice) {
		t.Errorf("should find %v in %v", trueID, slice)
	}

	slice = []string{bson.NewObjectId().Hex(), bson.NewObjectId().Hex(), trueID, bson.NewObjectId().Hex()}
	if !IDInSlice(trueID, slice) {
		t.Errorf("should find %v in %v", trueID, slice)
	}

	slice = []string{bson.NewObjectId().Hex(), bson.NewObjectId().Hex(), bson.NewObjectId().Hex(), trueID}
	if !IDInSlice(trueID, slice) {
		t.Errorf("should find %v in %v", trueID, slice)
	}

	falseID := bson.NewObjectId().Hex()
	if IDInSlice(falseID, slice) {
		t.Errorf("should not find %v in %v", trueID, slice)
	}

}

func TestSliceWithoutID(t *testing.T) {

	a := bson.NewObjectId().Hex()
	b := bson.NewObjectId().Hex()
	c := bson.NewObjectId().Hex()
	d := bson.NewObjectId().Hex()
	z := bson.NewObjectId().Hex()
	slice := []string{a, b, c, d}

	result := SliceWithoutID(slice, a)
	if !reflect.DeepEqual([]string{b, c, d}, result) {
		t.Errorf("error removing a from {a, b, c, d}, got: %v", result)
	}
	result = SliceWithoutID(slice, b)
	if !reflect.DeepEqual([]string{a, c, d}, result) {
		t.Errorf("error removing b from {a, b, c, d}, got: %v", result)
	}
	result = SliceWithoutID(slice, d)
	if !reflect.DeepEqual([]string{a, b, c}, result) {
		t.Errorf("error removing c from {a, b, c, d}, got: %v", result)
	}
	result = SliceWithoutID(slice, z)
	if !reflect.DeepEqual([]string{a, b, c, d}, result) {
		t.Errorf("error removing non-existent z from {a, b, c, d}, got: %v", result)
	}
}

func TestStringInSlice(t *testing.T) {

	trueID := bson.NewObjectId().Hex()

	slice := []string{trueID, bson.NewObjectId().Hex(), bson.NewObjectId().Hex(), bson.NewObjectId().Hex()}
	if !StringInSlice(trueID, slice) {
		t.Errorf("should find %v in %v", trueID, slice)
	}

	slice = []string{bson.NewObjectId().Hex(), bson.NewObjectId().Hex(), trueID, bson.NewObjectId().Hex()}
	if !StringInSlice(trueID, slice) {
		t.Errorf("should find %v in %v", trueID, slice)
	}

	slice = []string{bson.NewObjectId().Hex(), bson.NewObjectId().Hex(), bson.NewObjectId().Hex(), trueID}
	if !StringInSlice(trueID, slice) {
		t.Errorf("should find %v in %v", trueID, slice)
	}

	falseID := bson.NewObjectId().Hex()
	if StringInSlice(falseID, slice) {
		t.Errorf("should not find %v in %v", trueID, slice)
	}

}

func Test_imageHash(t *testing.T) {

	expectedSha256 := "sha256.d7d356079af905c04e5ae10711ecf3f5b34385e9b143c5d9ddbf740665ce2fb7"

	_, err := ImageHash("no_such_file.txt")
	if err == nil {
		t.Error("Invalid file must return an error")
	}

	shasum, err := ImageHash("test_data/test_sha256")
	if err != nil {
		t.Errorf("ImageHash on valid file should not raise error: %v", err)
	}
	if shasum != expectedSha256 {
		t.Errorf("ImageHash returned %v - expected %v", shasum, expectedSha256)
	}
}

func Test_sha256sum(t *testing.T) {

	expectedSha256 := "sha256.d7d356079af905c04e5ae10711ecf3f5b34385e9b143c5d9ddbf740665ce2fb7"

	_, err := sha256sum("no_such_file.txt")
	if err == nil {
		t.Error("Invalid file must return an error")
	}

	shasum, err := sha256sum("test_data/test_sha256")
	if err != nil {
		t.Errorf("sha256sum on valid file should not raise error: %v", err)
	}
	if shasum != expectedSha256 {
		t.Errorf("sha256sum returned %v - expected %v", shasum, expectedSha256)
	}
}
