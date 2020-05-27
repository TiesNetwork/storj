// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package admin

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/zeebo/errs"

	"storj.io/common/memory"
	"storj.io/storj/satellite/admin/adminql"
)

// ContentLengthLimit describes 4KB limit
const ContentLengthLimit = 4 * memory.KB

// JSON request from graphql clients
type graphqlJSON struct {
	Query         string
	OperationName string
	Variables     map[string]interface{}
}

// getQuery retrieves graphql query from request
func getQuery(w http.ResponseWriter, req *http.Request) (query graphqlJSON, err error) {
	switch req.Method {
	case http.MethodGet:
		query.Query = req.URL.Query().Get(adminql.Query)
		return query, nil
	case http.MethodPost:
		return queryPOST(w, req)
	default:
		return query, errs.New("wrong http request type")
	}
}

// queryPOST retrieves graphql query from POST request
func queryPOST(w http.ResponseWriter, req *http.Request) (query graphqlJSON, err error) {
	limitedReader := http.MaxBytesReader(w, req.Body, ContentLengthLimit.Int64())
	switch typ := req.Header.Get(contentType); typ {
	case applicationGraphql:
		body, err := ioutil.ReadAll(limitedReader)
		query.Query = string(body)
		return query, errs.Combine(err, limitedReader.Close())
	case applicationJSON:
		err := json.NewDecoder(limitedReader).Decode(&query)
		return query, errs.Combine(err, limitedReader.Close())
	default:
		return query, errs.New("can't parse request body of type %s", typ)
	}
}
