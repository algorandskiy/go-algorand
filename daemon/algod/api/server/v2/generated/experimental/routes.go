// Package experimental provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/algorand/oapi-codegen DO NOT EDIT.
package experimental

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"strings"

	. "github.com/algorand/go-algorand/daemon/algod/api/server/v2/generated/model"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
)

// ServerInterface represents all server handlers.
type ServerInterface interface {
	// Returns OK if experimental API is enabled.
	// (GET /v2/experimental)
	ExperimentalCheck(ctx echo.Context) error
	// Fast track for broadcasting a raw transaction or transaction group to the network through the tx handler without performing most of the checks and reporting detailed errors. Should be only used for development and performance testing.
	// (POST /v2/transactions/async)
	RawTransactionAsync(ctx echo.Context) error
}

// ServerInterfaceWrapper converts echo contexts to parameters.
type ServerInterfaceWrapper struct {
	Handler ServerInterface
}

// ExperimentalCheck converts echo context to params.
func (w *ServerInterfaceWrapper) ExperimentalCheck(ctx echo.Context) error {
	var err error

	ctx.Set(Api_keyScopes, []string{""})

	// Invoke the callback with all the unmarshalled arguments
	err = w.Handler.ExperimentalCheck(ctx)
	return err
}

// RawTransactionAsync converts echo context to params.
func (w *ServerInterfaceWrapper) RawTransactionAsync(ctx echo.Context) error {
	var err error

	ctx.Set(Api_keyScopes, []string{""})

	// Invoke the callback with all the unmarshalled arguments
	err = w.Handler.RawTransactionAsync(ctx)
	return err
}

// This is a simple interface which specifies echo.Route addition functions which
// are present on both echo.Echo and echo.Group, since we want to allow using
// either of them for path registration
type EchoRouter interface {
	CONNECT(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
	DELETE(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
	GET(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
	HEAD(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
	OPTIONS(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
	PATCH(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
	POST(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
	PUT(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
	TRACE(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
}

// RegisterHandlers adds each server route to the EchoRouter.
func RegisterHandlers(router EchoRouter, si ServerInterface, m ...echo.MiddlewareFunc) {
	RegisterHandlersWithBaseURL(router, si, "", m...)
}

// Registers handlers, and prepends BaseURL to the paths, so that the paths
// can be served under a prefix.
func RegisterHandlersWithBaseURL(router EchoRouter, si ServerInterface, baseURL string, m ...echo.MiddlewareFunc) {

	wrapper := ServerInterfaceWrapper{
		Handler: si,
	}

	router.GET(baseURL+"/v2/experimental", wrapper.ExperimentalCheck, m...)
	router.POST(baseURL+"/v2/transactions/async", wrapper.RawTransactionAsync, m...)

}

// Base64 encoded, gzipped, json marshaled Swagger object
var swaggerSpec = []string{

	"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",
	"JZpUkq0x7+7Hz/83AAD//8gL9x7v8QAA",
}

// GetSwagger returns the content of the embedded swagger specification file
// or error if failed to decode
func decodeSpec() ([]byte, error) {
	zipped, err := base64.StdEncoding.DecodeString(strings.Join(swaggerSpec, ""))
	if err != nil {
		return nil, fmt.Errorf("error base64 decoding spec: %s", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %s", err)
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(zr)
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %s", err)
	}

	return buf.Bytes(), nil
}

var rawSpec = decodeSpecCached()

// a naive cached of a decoded swagger spec
func decodeSpecCached() func() ([]byte, error) {
	data, err := decodeSpec()
	return func() ([]byte, error) {
		return data, err
	}
}

// Constructs a synthetic filesystem for resolving external references when loading openapi specifications.
func PathToRawSpec(pathToFile string) map[string]func() ([]byte, error) {
	var res = make(map[string]func() ([]byte, error))
	if len(pathToFile) > 0 {
		res[pathToFile] = rawSpec
	}

	return res
}

// GetSwagger returns the Swagger specification corresponding to the generated code
// in this file. The external references of Swagger specification are resolved.
// The logic of resolving external references is tightly connected to "import-mapping" feature.
// Externally referenced files must be embedded in the corresponding golang packages.
// Urls can be supported but this task was out of the scope.
func GetSwagger() (swagger *openapi3.T, err error) {
	var resolvePath = PathToRawSpec("")

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.ReadFromURIFunc = func(loader *openapi3.Loader, url *url.URL) ([]byte, error) {
		var pathToFile = url.String()
		pathToFile = path.Clean(pathToFile)
		getSpec, ok := resolvePath[pathToFile]
		if !ok {
			err1 := fmt.Errorf("path not found: %s", pathToFile)
			return nil, err1
		}
		return getSpec()
	}
	var specData []byte
	specData, err = rawSpec()
	if err != nil {
		return
	}
	swagger, err = loader.LoadFromData(specData)
	if err != nil {
		return
	}
	return
}
