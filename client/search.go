// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Search searches library by name, returns any matching collections,
// containers, entities, or images.
func (c *Client) Search(ctx context.Context, value string) (*SearchResults, error) {
	url := fmt.Sprintf("/v1/search?value=%s", url.QueryEscape(value))

	resJSON, _, err := c.apiGet(ctx, url)
	if err != nil {
		return nil, err
	}

	var res SearchResponse
	if err := json.Unmarshal(resJSON, &res); err != nil {
		return nil, fmt.Errorf("error decoding results: %v", err)
	}

	return &res.Data, nil
}

// searchLibrary will search the library for a given query and display results
func (c *Client) searchLibrary(ctx context.Context, value string) error {
	if len(value) < 3 {
		return fmt.Errorf("Bad query '%s'. You must search for at least 3 characters", value)
	}

	results, err := c.Search(ctx, value)
	if err != nil {
		return err
	}

	numEntities := len(results.Entities)
	numCollections := len(results.Collections)
	numContainers := len(results.Containers)

	if numEntities > 0 {
		c.Infof("Found %d users for '%s'", numEntities, value)
		for _, ent := range results.Entities {
			c.Infof("\t%s\n", ent.LibraryURI())
		}
	} else {
		c.Infof("No users found for '%s'", value)
	}

	if numCollections > 0 {
		c.Infof("Found %d collections for '%s'", numCollections, value)
		for _, col := range results.Collections {
			c.Infof("\t%s", col.LibraryURI())
		}
	} else {
		c.Infof("No collections found for '%s'", value)
	}

	if numContainers > 0 {
		c.Infof("Found %d containers for '%s'", numContainers, value)
		for _, con := range results.Containers {
			c.Infof("\t%s", con.LibraryURI())
			c.Infof("\t\tTags: %s", con.TagList())
		}
	} else {
		c.Infof("No containers found for '%s'", value)
	}

	return nil
}
