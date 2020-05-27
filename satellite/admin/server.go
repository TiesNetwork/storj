// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

// Package admin implements administrative endpoints for satellite.
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"storj.io/storj/satellite/admin/adminql"
	"storj.io/storj/satellite/admin/service"
)

const (
	contentType = "Content-Type"

	accessControlRequestHeaders   = "Access-Control-Request-Headers"
	accessControlAllowOrigin      = "Access-Control-Allow-Origin"
	accessControlAllowCredentials = "Access-Control-Allow-Credentials"
	accessControlAllowMethods     = "Access-Control-Allow-Methods"
	accessControlAllowHeaders     = "Access-Control-Allow-Headers"

	applicationJSON    = "application/json"
	applicationGraphql = "application/graphql"
)

var (
	// Error is satellite admin error type
	Error = errs.Class("satellite admin error")
)

// Config defines configuration for administration server.
type Config struct {
	Address         string `help:"admin peer http listening address" releaseDefault:"" devDefault:""`
	ExternalAddress string `help:"external endpoint of the satellite if hosted" default:""`
}

// Server provides endpoints for administration.
type Server struct {
	log *zap.Logger

	listener net.Listener
	server   http.Server

	config  Config
	service *service.Service

	schema graphql.Schema
}

// NewServer returns a new admin.Server.
func NewServer(log *zap.Logger, listener net.Listener, config Config, service *service.Service) *Server {
	server := &Server{
		log:     log,
		config:  config,
		service: service,
	}

	router := mux.NewRouter()

	{ // Setup account administration
		router.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Hello dolly! %s", r.RequestURI)
		})
	}

	//router.Handle("/api/v0/graphql", server.withAuth(http.HandlerFunc(server.grapqlHandler)))
	router.Handle("/api/v0/graphql", http.HandlerFunc(server.grapqlHandler))

	server.listener = listener
	server.server.Handler = router

	return server
}

// Run starts the admin endpoint.
func (server *Server) Run(ctx context.Context) (err error) {
	if server.listener == nil {
		return nil
	}

	server.schema, err = adminql.CreateSchema(server.log, server.service)
	if err != nil {
		return Error.Wrap(err)
	}

	ctx, cancel := context.WithCancel(ctx)
	var group errgroup.Group
	group.Go(func() error {
		<-ctx.Done()
		return Error.Wrap(server.server.Shutdown(context.Background()))
	})
	group.Go(func() error {
		defer cancel()
		return Error.Wrap(server.server.Serve(server.listener))
	})
	return group.Wait()
}

// Close closes server and underlying listener.
func (server *Server) Close() error {
	return Error.Wrap(server.server.Close())
}

// grapqlHandler is graphql endpoint http handler function
func (server *Server) grapqlHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	handleError := func(code int, err error) {
		w.WriteHeader(code)

		var jsonError struct {
			Error string `json:"error"`
		}

		jsonError.Error = err.Error()

		if err := json.NewEncoder(w).Encode(jsonError); err != nil {
			server.log.Error("error graphql error", zap.Error(err))
		}
	}

	w.Header().Set(contentType, applicationJSON)
	w.Header().Set(accessControlAllowCredentials, "true")
	origins := r.Header["Origin"]
	if len(origins) <= 0 {
		origins = []string{"*"}
	}
	w.Header().Set(accessControlAllowOrigin, origins[0])

	switch r.Method {
	case http.MethodOptions:
		w.Header().Set(accessControlAllowMethods, "POST, GET, OPTIONS")
		headers := r.Header[accessControlRequestHeaders]
		if len(headers) > 0 {
			w.Header().Set(accessControlAllowHeaders, headers[0])
		}
		return
	}

	query, err := getQuery(w, r)
	if err != nil {
		handleError(http.StatusBadRequest, err)
		return
	}

	rootObject := make(map[string]interface{})

	result := graphql.Do(graphql.Params{
		Schema:         server.schema,
		Context:        ctx,
		RequestString:  query.Query,
		VariableValues: query.Variables,
		OperationName:  query.OperationName,
		RootObject:     rootObject,
	})

	getGqlError := func(err gqlerrors.FormattedError) error {
		if gerr, ok := err.OriginalError().(*gqlerrors.Error); ok {
			return gerr.OriginalError
		}

		return nil
	}

	parseConsoleError := func(err error) (int, error) {
		switch {
		case service.ErrUnauthorized.Has(err):
			return http.StatusUnauthorized, err
		case service.ErrValidation.Has(err):
			return http.StatusBadRequest, err
		case service.Error.Has(err):
			return http.StatusInternalServerError, err
		}

		return 0, nil
	}

	handleErrors := func(code int, errors gqlerrors.FormattedErrors) {
		w.WriteHeader(code)

		var jsonError struct {
			Errors []string `json:"errors"`
		}

		for _, err := range errors {
			jsonError.Errors = append(jsonError.Errors, err.Message)
		}

		if err := json.NewEncoder(w).Encode(jsonError); err != nil {
			server.log.Error("error graphql error", zap.Error(err))
		}
	}

	handleGraphqlErrors := func() {
		for _, err := range result.Errors {
			gqlErr := getGqlError(err)
			if gqlErr == nil {
				continue
			}

			code, err := parseConsoleError(gqlErr)
			if err != nil {
				handleError(code, err)
				return
			}
		}

		handleErrors(http.StatusBadRequest, result.Errors)
	}

	if result.HasErrors() {
		handleGraphqlErrors()
		return
	}

	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		server.log.Error("error encoding grapql result", zap.Error(err))
		return
	}

	//server.log.Sugar().Debug(result)
}
