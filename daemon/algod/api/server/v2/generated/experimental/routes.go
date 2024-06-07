// Package experimental provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/algorand/oapi-codegen DO NOT EDIT.
package experimental

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	. "github.com/algorand/go-algorand/daemon/algod/api/server/v2/generated/model"
	"github.com/algorand/oapi-codegen/pkg/runtime"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
)

// ServerInterface represents all server handlers.
type ServerInterface interface {
	// Get a list of assets held by an account, inclusive of asset params.
	// (GET /v2/accounts/{address}/assets)
	AccountAssetsInformation(ctx echo.Context, address string, params AccountAssetsInformationParams) error
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

// AccountAssetsInformation converts echo context to params.
func (w *ServerInterfaceWrapper) AccountAssetsInformation(ctx echo.Context) error {
	var err error
	// ------------- Path parameter "address" -------------
	var address string

	err = runtime.BindStyledParameterWithLocation("simple", false, "address", runtime.ParamLocationPath, ctx.Param("address"), &address)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter address: %s", err))
	}

	ctx.Set(Api_keyScopes, []string{""})

	// Parameter object where we will unmarshal all parameters from the context
	var params AccountAssetsInformationParams
	// ------------- Optional query parameter "limit" -------------

	err = runtime.BindQueryParameter("form", true, false, "limit", ctx.QueryParams(), &params.Limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter limit: %s", err))
	}

	// ------------- Optional query parameter "next" -------------

	err = runtime.BindQueryParameter("form", true, false, "next", ctx.QueryParams(), &params.Next)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter next: %s", err))
	}

	// Invoke the callback with all the unmarshalled arguments
	err = w.Handler.AccountAssetsInformation(ctx, address, params)
	return err
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

	router.GET(baseURL+"/v2/accounts/:address/assets", wrapper.AccountAssetsInformation, m...)
	router.GET(baseURL+"/v2/experimental", wrapper.ExperimentalCheck, m...)
	router.POST(baseURL+"/v2/transactions/async", wrapper.RawTransactionAsync, m...)

}

// Base64 encoded, gzipped, json marshaled Swagger object
var swaggerSpec = []string{

	"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",
	"Zx//fwAAAP///4b+Y68HAQA=",
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
