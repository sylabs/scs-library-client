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

// Search searches library, returns any matching collections, containers,
// entities, or images.
//
// args is key-value pairs, such as "arch" (ie. "amd64") or "signed" ("true" or
// "false"). "value" is the keyword argument and is required for a search.  It
// will be matched against all collections (Entity, Collection, Container, and
// Image)
func (c *Client) Search(ctx context.Context, args map[string]string) (*SearchResults, error) {
	// "value" is minimally required in "args"
	value, ok := args["value"]
	if !ok {
		return nil, fmt.Errorf("search query ('value') must be specified")
	}

	if len(value) < 3 {
		return nil, fmt.Errorf("bad query '%s'. You must search for at least 3 characters", value)
	}

	v := url.Values{}
	for key, value := range args {
		v.Set(key, value)
	}

	resJSON, _, err := c.apiGet(ctx, "/v1/search?"+v.Encode())
	if err != nil {
		return nil, err
	}

	var res SearchResponse
	if err := json.Unmarshal(resJSON, &res); err != nil {
		return nil, fmt.Errorf("error decoding results: %v", err)
	}

	return &res.Data, nil
}
