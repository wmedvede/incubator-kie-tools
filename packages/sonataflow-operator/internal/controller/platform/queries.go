// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// {"errors":[
// {"message":"Validation error (FieldUndefined@[ProcessDefinitions/sources]) : Field 'sources' in type 'ProcessDefinition' is undefined",
// "locations":[{"line":1,"column":38}],
// "extensions":{"classification":"ValidationError"}
// }]}

// {"data":{"ProcessDefinitions":[]}}

type GraphQLQuery struct {
	Query string `json:"query"`
}

type GraphQLResponse struct {
	Data   map[string]any `json:"data"`
	Errors []interface{}  `json:"errors,omitempty"`
}

func ExecuteDataIndexQuery(ctx context.Context, graphqlUrl string, query GraphQLQuery) (*GraphQLResponse, error) {
	return postJSON(ctx, graphqlUrl, query)
}
func postJSON(ctx context.Context, url string, query GraphQLQuery) (*GraphQLResponse, error) {
	// encode request
	bodyBytes, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	// create HTTP request
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewBuffer(bodyBytes),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// HTTP client with timeout
	client := &http.Client{
		//TODO WM, we might have different timeout than the configured in the caller worker.
		Timeout: 5 * time.Second,
	}

	// execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// decode response
	var result GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
