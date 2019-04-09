// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/globalsign/mgo/bson"
	"github.com/golang/glog"
)

func getEntity(c *Client, entityRef string) (entity Entity, found bool, err error) {
	url := "/v1/entities/" + entityRef
	entJSON, found, err := c.apiGet(url)
	if err != nil {
		return entity, false, err
	}
	if !found {
		return entity, false, nil
	}
	var res EntityResponse
	if err := json.Unmarshal(entJSON, &res); err != nil {
		return entity, false, fmt.Errorf("error decoding entity: %v", err)
	}
	return res.Data, found, nil
}

func getCollection(c *Client, collectionRef string) (collection Collection, found bool, err error) {
	url := "/v1/collections/" + collectionRef
	colJSON, found, err := c.apiGet(url)
	if err != nil {
		return collection, false, err
	}
	if !found {
		return collection, false, nil
	}
	var res CollectionResponse
	if err := json.Unmarshal(colJSON, &res); err != nil {
		return collection, false, fmt.Errorf("error decoding collection: %v", err)
	}
	return res.Data, found, nil
}

func getContainer(c *Client, containerRef string) (container Container, found bool, err error) {
	url := "/v1/containers/" + containerRef
	conJSON, found, err := c.apiGet(url)
	if err != nil {
		return container, false, err
	}
	if !found {
		return container, false, nil
	}
	var res ContainerResponse
	if err := json.Unmarshal(conJSON, &res); err != nil {
		return container, false, fmt.Errorf("error decoding container: %v", err)
	}
	return res.Data, found, nil
}

func getImage(c *Client, imageRef string) (image Image, found bool, err error) {
	url := "/v1/images/" + imageRef
	imgJSON, found, err := c.apiGet(url)
	if err != nil {
		return image, false, err
	}
	if !found {
		return image, false, nil
	}
	var res ImageResponse
	if err := json.Unmarshal(imgJSON, &res); err != nil {
		return image, false, fmt.Errorf("error decoding image: %v", err)
	}
	return res.Data, found, nil
}

func createEntity(c *Client, name string) (entity Entity, err error) {
	e := Entity{
		Name:        name,
		Description: "No description",
	}
	entJSON, err := apiCreate(c, "/v1/entities", e)
	if err != nil {
		return entity, err
	}
	var res EntityResponse
	if err := json.Unmarshal(entJSON, &res); err != nil {
		return entity, fmt.Errorf("error decoding entity: %v", err)
	}
	return res.Data, nil
}

func createCollection(c *Client, name string, entityID string) (collection Collection, err error) {
	newCollection := Collection{
		Name:        name,
		Description: "No description",
		Entity:      bson.ObjectIdHex(entityID),
	}
	colJSON, err := apiCreate(c, "/v1/collections", newCollection)
	if err != nil {
		return newCollection, err
	}
	var res CollectionResponse
	if err := json.Unmarshal(colJSON, &res); err != nil {
		return collection, fmt.Errorf("error decoding collection: %v", err)
	}
	return res.Data, nil
}

func createContainer(c *Client, name string, collectionID string) (container Container, err error) {
	newContainer := Container{
		Name:        name,
		Description: "No description",
		Collection:  bson.ObjectIdHex(collectionID),
	}
	conJSON, err := apiCreate(c, "/v1/containers", newContainer)
	if err != nil {
		return newContainer, err
	}
	var res ContainerResponse
	if err := json.Unmarshal(conJSON, &res); err != nil {
		return container, fmt.Errorf("error decoding container: %v", err)
	}
	return res.Data, nil
}

func createImage(c *Client, hash string, containerID string, description string) (image Image, err error) {
	i := Image{
		Hash:        hash,
		Description: description,
		Container:   bson.ObjectIdHex(containerID),
	}
	imgJSON, err := apiCreate(c, "/v1/images", i)
	if err != nil {
		return image, err
	}
	var res ImageResponse
	if err := json.Unmarshal(imgJSON, &res); err != nil {
		return image, fmt.Errorf("error decoding image: %v", err)
	}
	return res.Data, nil
}

func setTags(c *Client, containerID, imageID string, tags []string) error {
	// Get existing tags, so we know which will be replaced
	existingTags, err := apiGetTags(c, "/v1/tags/"+containerID)
	if err != nil {
		return err
	}

	for _, tag := range tags {
		glog.Infof("Setting tag %s", tag)

		if _, ok := existingTags[tag]; ok {
			glog.Warningf("%s replaces an existing tag", tag)
		}

		imgTag := ImageTag{
			tag,
			bson.ObjectIdHex(imageID),
		}
		err := apiSetTag(c, "/v1/tags/"+containerID, imgTag)
		if err != nil {
			return err
		}
	}
	return nil
}

func search(c *Client, value string) (results SearchResults, err error) {
	u, err := url.Parse("/v1/search")
	if err != nil {
		return
	}
	q := u.Query()
	q.Set("value", value)
	u.RawQuery = q.Encode()

	resJSON, _, err := c.apiGet(u.String())
	if err != nil {
		return results, err
	}

	var res SearchResponse
	if err := json.Unmarshal(resJSON, &res); err != nil {
		return results, fmt.Errorf("error decoding results: %v", err)
	}

	return res.Data, nil
}

func apiCreate(c *Client, url string, o interface{}) (objJSON []byte, err error) {
	glog.V(2).Infof("apiCreate calling %s", url)
	s, err := json.Marshal(o)
	if err != nil {
		return []byte{}, fmt.Errorf("error encoding object to JSON:\n\t%v", err)
	}
	req, err := c.NewRequest("POST", url, "", bytes.NewBuffer(s))
	if err != nil {
		return []byte{}, fmt.Errorf("error creating POST request:\n\t%v", err)
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return []byte{}, fmt.Errorf("error making request to server:\n\t%v", err)
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		jRes, err := ParseErrorBody(res.Body)
		if err != nil {
			jRes = ParseErrorResponse(res)
		}
		return []byte{}, fmt.Errorf("creation did not succeed: %d %s\n\t%v",
			jRes.Error.Code, jRes.Error.Status, jRes.Error.Message)
	}
	objJSON, err = ioutil.ReadAll(res.Body)
	if err != nil {
		return []byte{}, fmt.Errorf("error reading response from server:\n\t%v", err)
	}
	return objJSON, nil
}

func (c *Client) apiGet(url string) (objJSON []byte, found bool, err error) {
	glog.V(2).Infof("apiGet calling %s", url)
	req, err := c.NewRequest(http.MethodGet, url, "", nil)
	if err != nil {
		return []byte{}, false, fmt.Errorf("error creating request to server:\n\t%v", err)
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return []byte{}, false, fmt.Errorf("error making request to server:\n\t%v", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return []byte{}, false, nil
	}
	if res.StatusCode == http.StatusOK {
		objJSON, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return []byte{}, false, fmt.Errorf("error reading response from server:\n\t%v", err)
		}
		return objJSON, true, nil
	}
	// Not OK, not 404.... error
	jRes, err := ParseErrorBody(res.Body)
	if err != nil {
		jRes = ParseErrorResponse(res)
	}
	return []byte{}, false, fmt.Errorf("get did not succeed: %d %s\n\t%v",
		jRes.Error.Code, jRes.Error.Status, jRes.Error.Message)
}

func apiGetTags(c *Client, url string) (tags TagMap, err error) {
	glog.V(2).Infof("apiGetTags calling %s", url)
	req, err := c.NewRequest(http.MethodGet, url, "", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request to server:\n\t%v", err)
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request to server:\n\t%v", err)
	}
	if res.StatusCode != http.StatusOK {
		jRes, err := ParseErrorBody(res.Body)
		if err != nil {
			jRes = ParseErrorResponse(res)
		}
		return nil, fmt.Errorf("creation did not succeed: %d %s\n\t%v",
			jRes.Error.Code, jRes.Error.Status, jRes.Error.Message)
	}
	var tagRes TagsResponse
	err = json.NewDecoder(res.Body).Decode(&tagRes)
	if err != nil {
		return tags, fmt.Errorf("error decoding tags: %v", err)
	}
	return tagRes.Data, nil

}

func apiSetTag(c *Client, url string, t ImageTag) (err error) {
	glog.V(2).Infof("apiSetTag calling %s", url)
	s, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("error encoding object to JSON:\n\t%v", err)
	}
	req, err := c.NewRequest("POST", url, "", bytes.NewBuffer(s))
	if err != nil {
		return fmt.Errorf("error creating POST request:\n\t%v", err)
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("error making request to server:\n\t%v", err)
	}
	if res.StatusCode != http.StatusOK {
		jRes, err := ParseErrorBody(res.Body)
		if err != nil {
			jRes = ParseErrorResponse(res)
		}
		return fmt.Errorf("creation did not succeed: %d %s\n\t%v",
			jRes.Error.Code, jRes.Error.Status, jRes.Error.Message)
	}
	return nil
}

// GetImage returns the Image object if exists, otherwise returns error
func GetImage(c *Client, imageRef string) (image Image, err error) {
	entityName, collectionName, containerName, tags := parseLibraryRef(imageRef)

	i, f, err := getImage(c, entityName+"/"+collectionName+"/"+containerName+":"+tags[0])
	if err != nil {
		return Image{}, err
	} else if !f {
		return Image{}, fmt.Errorf("the requested image was not found in the library")
	}

	return i, nil
}
