// Copyright (c) 2018-2023, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	jsonresp "github.com/sylabs/json-resp"
)

const (
	testToken = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.TCYt5XsITJX1CxPCT8yAV-TVkIEq_PbChOMqsLfRoPsnsgw5WEuts01mq-pQy7UJiN5mgRxD-WUcX16dUEMGlv50aqzpqh4Qktb3rk-BuQy72IFLOqV0G_zS245-kronKb78cPN25DGlcTwLtjPAYuNzVBAh4vGHSrQyHUdBBPM"
)

var (
	signedImage      = true
	unsignedImage    = false
	encryptedImage   = true
	unencryptedImage = false

	testEntity = Entity{
		ID:          "5cb9c34d7d960d82f5f5bc4a",
		Name:        "test-user",
		Description: "A test user",
	}

	testCollection = Collection{
		ID:          "5cb9c34d7d960d82f5f5bc4b",
		Name:        "test-collection",
		Description: "A test collection",
		Entity:      testEntity.ID,
		EntityName:  testEntity.Name,
	}

	testContainer = Container{
		ID:             "5cb9c34d7d960d82f5f5bc4c",
		Name:           "test-container",
		Description:    "A test container",
		Entity:         testEntity.ID,
		EntityName:     testEntity.Name,
		Collection:     testEntity.ID,
		CollectionName: testCollection.Name,
		ImageTags: map[string]string{
			"test-tag": "5cb9c34d7d960d82f5f5bc4d",
			"latest":   "5cb9c34d7d960d82f5f5bc4e",
		},
	}

	archIntel = "amd64"

	testImage = Image{
		ID:             "5cb9c34d7d960d82f5f5bc4f",
		Hash:           "sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88",
		Entity:         testEntity.ID,
		EntityName:     testEntity.Name,
		Collection:     testEntity.ID,
		CollectionName: testCollection.Name,
		Container:      testContainer.ID,
		ContainerName:  testContainer.Name,
		Architecture:   &archIntel,
	}

	archARM = "arm64"

	testImage2 = Image{
		ID:             "bf396e3d2de63215e731c11f",
		Hash:           "sha256.d8fb363e56735af5f127a2f12bdba8d7aedf5861c7ef7eb7197f56323d1831f7",
		Entity:         testEntity.ID,
		EntityName:     testEntity.Name,
		Collection:     testEntity.ID,
		CollectionName: testCollection.Name,
		Container:      testContainer.ID,
		ContainerName:  testContainer.Name,
		Architecture:   &archARM,
	}

	testImage3 = Image{
		ID:             "49a312c677e2e3f3d36ac3d0",
		Hash:           "sha256.b23a9b9f41809a2dfe16a9e3b1d948909dab2efbf1997d6f5a46dea8af5cdb78",
		Entity:         testEntity.ID,
		EntityName:     testEntity.Name,
		Collection:     testEntity.ID,
		CollectionName: testCollection.Name,
		Container:      testContainer.ID,
		ContainerName:  testContainer.Name,
		Architecture:   &archARM,
		Signed:         &signedImage,
	}

	testImage4 = Image{
		ID:             "2c2e6c834274f031a01f83dc",
		Hash:           "sha256.4f4ba1a6b584001734a33247a06e75d06af2999103139a2e2b767dc873a21b7a",
		Entity:         testEntity.ID,
		EntityName:     testEntity.Name,
		Collection:     testEntity.ID,
		CollectionName: testCollection.Name,
		Container:      testContainer.ID,
		ContainerName:  testContainer.Name,
		Architecture:   &archARM,
		Signed:         &unsignedImage,
	}

	testImage5 = Image{
		ID:             "1111222223333031a01f83dc",
		Hash:           "sha256.4f4ba1112223331734a33247a06e75d06af2999103139a2e2b767dc873a21b7a",
		Entity:         testEntity.ID,
		EntityName:     testEntity.Name,
		Collection:     testEntity.ID,
		CollectionName: testCollection.Name,
		Container:      testContainer.ID,
		ContainerName:  testContainer.Name,
		Signed:         &unsignedImage,
	}

	testImage6 = Image{
		ID:             "222233334444031a01f83dc",
		Hash:           "sha256.4f4ba1112223331734a33247a06e75d06af2999103139a2e2b767dc873a21b7a",
		Entity:         testEntity.ID,
		EntityName:     testEntity.Name,
		Collection:     testEntity.ID,
		CollectionName: testCollection.Name,
		Container:      testContainer.ID,
		ContainerName:  testContainer.Name,
		Signed:         &unsignedImage,
		Encrypted:      &unencryptedImage,
	}

	testImage7 = Image{
		ID:             "333344445555031a01f83dc",
		Hash:           "sha256.4f4ba1112223331734a33247a06e75d06af2999103139a2e2b767dc873a21b7a",
		Entity:         testEntity.ID,
		EntityName:     testEntity.Name,
		Collection:     testEntity.ID,
		CollectionName: testCollection.Name,
		Container:      testContainer.ID,
		ContainerName:  testContainer.Name,
		Signed:         &unsignedImage,
		Encrypted:      &encryptedImage,
	}

	testSearch = SearchResults{
		Entities:    []Entity{testEntity},
		Collections: []Collection{testCollection},
		Containers:  []Container{testContainer},
		Images: []Image{
			testImage, testImage2, testImage3, testImage4,
			testImage5, testImage6, testImage7,
		},
	}
)

type mockService struct {
	t           *testing.T
	code        int
	body        interface{}
	reqCallback func(*http.Request, *testing.T)
	httpAddr    string
	httpPath    string
	httpServer  *httptest.Server
	baseURI     string
}

func (m *mockService) Run() {
	mux := http.NewServeMux()
	mux.HandleFunc(m.httpPath, m.ServeHTTP)
	m.httpServer = httptest.NewServer(mux)
	m.httpAddr = m.httpServer.Listener.Addr().String()
	m.baseURI = "http://" + m.httpAddr
}

func (m *mockService) Stop() {
	m.httpServer.Close()
}

func (m *mockService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.reqCallback != nil {
		m.reqCallback(r, m.t)
	}
	w.WriteHeader(m.code)
	err := json.NewEncoder(w).Encode(&m.body)
	if err != nil {
		m.t.Errorf("Error encoding mock response: %v", err)
	}
}

func Test_getEntity(t *testing.T) {
	tests := []struct {
		description  string
		code         int
		body         interface{}
		reqCallback  func(*http.Request, *testing.T)
		entityRef    string
		expectEntity *Entity
		expectFound  bool
		expectError  bool
	}{
		{
			description:  "NotFound",
			code:         http.StatusNotFound,
			body:         jsonresp.Response{Error: &jsonresp.Error{Code: http.StatusNotFound}},
			reqCallback:  nil,
			entityRef:    "notthere",
			expectEntity: nil,
			expectFound:  false,
			expectError:  true,
		},
		{
			description:  "Unauthorized",
			code:         http.StatusUnauthorized,
			body:         jsonresp.Response{Error: &jsonresp.Error{Code: http.StatusUnauthorized}},
			reqCallback:  nil,
			entityRef:    "notmine",
			expectEntity: nil,
			expectFound:  false,
			expectError:  true,
		},
		{
			description:  "ValidResponse",
			code:         http.StatusOK,
			body:         EntityResponse{Data: testEntity},
			reqCallback:  nil,
			entityRef:    "test-user",
			expectEntity: &testEntity,
			expectFound:  true,
			expectError:  false,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				body:        tt.body,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/entities/" + tt.entityRef,
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			entity, err := c.getEntity(context.Background(), tt.entityRef)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if err != nil && errors.Is(err, ErrNotFound) && tt.expectFound {
				t.Errorf("Got found %v - expected %v", !errors.Is(err, ErrNotFound), tt.expectFound)
			}
			if !reflect.DeepEqual(entity, tt.expectEntity) {
				t.Errorf("Got entity %v - expected %v", entity, tt.expectEntity)
			}
		})
	}
}

func Test_getCollection(t *testing.T) {
	tests := []struct {
		description      string
		code             int
		body             interface{}
		reqCallback      func(*http.Request, *testing.T)
		collectionRef    string
		expectCollection *Collection
		expectFound      bool
		expectError      bool
	}{
		{
			description:      "NotFound",
			code:             http.StatusNotFound,
			body:             jsonresp.Response{Error: &jsonresp.Error{Code: http.StatusNotFound}},
			reqCallback:      nil,
			collectionRef:    "notthere",
			expectCollection: nil,
			expectFound:      false,
			expectError:      true,
		},
		{
			description:      "Unauthorized",
			code:             http.StatusUnauthorized,
			body:             jsonresp.Response{Error: &jsonresp.Error{Code: http.StatusUnauthorized}},
			reqCallback:      nil,
			collectionRef:    "notmine",
			expectCollection: nil,
			expectFound:      false,
			expectError:      true,
		},
		{
			description:      "ValidResponse",
			code:             http.StatusOK,
			body:             CollectionResponse{Data: testCollection},
			reqCallback:      nil,
			collectionRef:    "test-entity/test-collection",
			expectCollection: &testCollection,
			expectFound:      true,
			expectError:      false,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				body:        tt.body,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/collections/" + tt.collectionRef,
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			collection, err := c.getCollection(context.Background(), tt.collectionRef)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if err != nil && errors.Is(err, ErrNotFound) && tt.expectFound {
				t.Errorf("Got found %v - expected %v", !errors.Is(err, ErrNotFound), tt.expectFound)
			}
			if !reflect.DeepEqual(collection, tt.expectCollection) {
				t.Errorf("Got entity %v - expected %v", collection, tt.expectCollection)
			}
		})
	}
}

func Test_getContainer(t *testing.T) {
	tests := []struct {
		description     string
		code            int
		body            interface{}
		reqCallback     func(*http.Request, *testing.T)
		containerRef    string
		expectContainer *Container
		expectFound     bool
		expectError     bool
	}{
		{
			description:     "NotFound",
			code:            http.StatusNotFound,
			body:            jsonresp.Response{Error: &jsonresp.Error{Code: http.StatusNotFound}},
			reqCallback:     nil,
			containerRef:    "notthere",
			expectContainer: nil,
			expectFound:     false,
			expectError:     true,
		},
		{
			description:     "Unauthorized",
			code:            http.StatusUnauthorized,
			body:            jsonresp.Response{Error: &jsonresp.Error{Code: http.StatusUnauthorized}},
			reqCallback:     nil,
			containerRef:    "notmine",
			expectContainer: nil,
			expectFound:     false,
			expectError:     true,
		},
		{
			description:     "ValidResponse",
			code:            http.StatusOK,
			body:            ContainerResponse{Data: testContainer},
			reqCallback:     nil,
			containerRef:    "test-entity/test-collection/test-container",
			expectContainer: &testContainer,
			expectFound:     true,
			expectError:     false,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				body:        tt.body,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/containers/" + tt.containerRef,
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			container, err := c.getContainer(context.Background(), tt.containerRef)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if err != nil && !errors.Is(err, ErrNotFound) && tt.expectFound {
				t.Errorf("Got found %v - expected %v", !errors.Is(err, ErrNotFound), tt.expectFound)
			}
			if !reflect.DeepEqual(container, tt.expectContainer) {
				t.Errorf("Got container %v - expected %v", container, tt.expectContainer)
			}
		})
	}
}

func Test_getImage(t *testing.T) {
	tests := []struct {
		description string
		code        int
		body        interface{}
		reqCallback func(*http.Request, *testing.T)
		arch        string
		imageRef    string
		expectImage *Image
		expectFound bool
		expectError bool
	}{
		{
			description: "NotFound",
			code:        http.StatusNotFound,
			body:        jsonresp.Response{Error: &jsonresp.Error{Code: http.StatusNotFound}},
			reqCallback: nil,
			arch:        archIntel,
			imageRef:    "notthere",
			expectImage: nil,
			expectFound: false,
			expectError: true,
		},
		{
			description: "Unauthorized",
			code:        http.StatusUnauthorized,
			body:        jsonresp.Response{Error: &jsonresp.Error{Code: http.StatusUnauthorized}},
			reqCallback: nil,
			arch:        archIntel,
			imageRef:    "notmine",
			expectImage: nil,
			expectFound: false,
			expectError: true,
		},
		{
			description: "ValidResponse",
			code:        http.StatusOK,
			body:        ImageResponse{Data: testImage},
			reqCallback: nil,
			arch:        archIntel,
			imageRef:    "test",
			expectImage: &testImage,
			expectFound: true,
			expectError: false,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				body:        tt.body,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/images/" + tt.imageRef,
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			image, err := c.GetImage(context.Background(), tt.arch, tt.imageRef)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if err != nil && !errors.Is(err, ErrNotFound) && tt.expectFound {
				t.Errorf("Got found %v - expected %v", !errors.Is(err, ErrNotFound), tt.expectFound)
			}
			if !reflect.DeepEqual(image, tt.expectImage) {
				t.Errorf("Got image %v - expected %v", image, tt.expectImage)
			}
		})
	}
}

func Test_createEntity(t *testing.T) {
	tests := []struct {
		description  string
		code         int
		body         interface{}
		reqCallback  func(*http.Request, *testing.T)
		entityRef    string
		expectEntity *Entity
		expectError  bool
	}{
		{
			description:  "Valid Request",
			code:         http.StatusOK,
			body:         EntityResponse{Data: testEntity},
			entityRef:    "test",
			expectEntity: &testEntity,
			expectError:  false,
		},
		{
			description:  "Error response",
			code:         http.StatusInternalServerError,
			body:         Entity{},
			entityRef:    "test",
			expectEntity: nil,
			expectError:  true,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				body:        tt.body,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/entities/",
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			entity, err := c.createEntity(context.Background(), tt.entityRef)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if !reflect.DeepEqual(entity, tt.expectEntity) {
				t.Errorf("Got created entity %v - expected %v", entity, tt.expectEntity)
			}
		})
	}
}

func Test_createCollection(t *testing.T) {
	tests := []struct {
		description      string
		code             int
		body             interface{}
		reqCallback      func(*http.Request, *testing.T)
		collectionRef    string
		expectCollection *Collection
		expectError      bool
	}{
		{
			description:      "Valid Request",
			code:             http.StatusOK,
			body:             CollectionResponse{Data: Collection{Name: "test"}},
			collectionRef:    "test",
			expectCollection: &Collection{Name: "test"},
			expectError:      false,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				body:        tt.body,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/collections/",
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			collection, err := c.createCollection(context.Background(), tt.collectionRef, "5cb9c34d7d960d82f5f5bc50")

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if !reflect.DeepEqual(collection, tt.expectCollection) {
				t.Errorf("Got created collection %v - expected %v", collection, tt.expectCollection)
			}
		})
	}
}

func Test_createContainer(t *testing.T) {
	tests := []struct {
		description     string
		code            int
		body            interface{}
		reqCallback     func(*http.Request, *testing.T)
		containerRef    string
		expectContainer *Container
		expectError     bool
	}{
		{
			description:     "Valid Request",
			code:            http.StatusOK,
			body:            ContainerResponse{Data: Container{Name: "test"}},
			containerRef:    "test",
			expectContainer: &Container{Name: "test"},
			expectError:     false,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				body:        tt.body,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/containers/",
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			container, err := c.createContainer(context.Background(), tt.containerRef, "5cb9c34d7d960d82f5f5bc51")

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if !reflect.DeepEqual(container, tt.expectContainer) {
				t.Errorf("Got created collection %v - expected %v", container, tt.expectContainer)
			}
		})
	}
}

func Test_createImage(t *testing.T) {
	tests := []struct {
		description string
		code        int
		body        interface{}
		reqCallback func(*http.Request, *testing.T)
		imageRef    string
		expectImage *Image
		expectError bool
	}{
		{
			description: "Valid Request",
			code:        http.StatusOK,
			body:        ImageResponse{Data: Image{Hash: "sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88"}},
			imageRef:    "test",
			expectImage: &Image{Hash: "sha256.e50a30881ace3d5944f5661d222db7bee5296be9e4dc7c1fcb7604bcae926e88"},
			expectError: false,
		},
		{
			description: "Error response",
			code:        http.StatusInternalServerError,
			body:        Image{},
			imageRef:    "test",
			expectImage: nil,
			expectError: true,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				body:        tt.body,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/images/",
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			image, err := c.createImage(context.Background(), tt.imageRef, "5cb9c34d7d960d82f5f5bc52", "No Description")

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
			if !reflect.DeepEqual(image, tt.expectImage) {
				t.Errorf("Got created collection %v - expected %v", image, tt.expectImage)
			}
		})
	}
}

func Test_setTags(t *testing.T) {
	tests := []struct {
		description  string
		code         int
		reqCallback  func(*http.Request, *testing.T)
		containerRef string
		imageRef     string
		tags         []string
		expectError  bool
	}{
		{
			description:  "Valid Request",
			code:         http.StatusOK,
			containerRef: "test",
			imageRef:     "5cb9c34d7d960d82f5f5bc53",
			tags:         []string{"tag1", "tag2", "tag3"},
			expectError:  false,
		},
		{
			description:  "Error response",
			code:         http.StatusInternalServerError,
			containerRef: "test",
			imageRef:     "5cb9c34d7d960d82f5f5bc54",
			tags:         []string{"tag1", "tag2", "tag3"},
			expectError:  true,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				reqCallback: tt.reqCallback,
				httpPath:    "/v1/tags/" + tt.containerRef,
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			err = c.setTags(context.Background(), tt.containerRef, tt.imageRef, tt.tags)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
		})
	}
}

func Test_setTagsV2(t *testing.T) {
	tests := []struct {
		description  string
		code         int
		reqCallback  func(*http.Request, *testing.T)
		containerRef string
		imageRef     string
		arch         string
		tags         []string
		expectError  bool
	}{
		{
			description:  "Valid Request",
			code:         http.StatusOK,
			containerRef: "test",
			imageRef:     "5cb9c34d7d960d82f5f5bc53",
			arch:         archIntel,
			tags:         []string{"tag1", "tag2", "tag3"},
			expectError:  false,
		},
		{
			description:  "Error response",
			code:         http.StatusInternalServerError,
			containerRef: "test",
			imageRef:     "5cb9c34d7d960d82f5f5bc54",
			arch:         archIntel,
			tags:         []string{"tag1", "tag2", "tag3"},
			expectError:  true,
		},
	}

	// Loop over test cases
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := mockService{
				t:           t,
				code:        tt.code,
				reqCallback: tt.reqCallback,
				httpPath:    "/v2/tags/" + tt.containerRef,
			}

			m.Run()
			defer m.Stop()

			c, err := NewClient(&Config{AuthToken: testToken, BaseURL: m.baseURI})
			if err != nil {
				t.Errorf("Error initializing client: %v", err)
			}

			err = c.setTagsV2(context.Background(), tt.containerRef, tt.imageRef, tt.arch, tt.tags)

			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && tt.expectError {
				t.Errorf("Unexpected success. Expected error.")
			}
		})
	}
}
