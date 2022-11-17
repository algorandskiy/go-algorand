// Package private provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/algorand/oapi-codegen DO NOT EDIT.
package private

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
	// Aborts a catchpoint catchup.
	// (DELETE /v2/catchup/{catchpoint})
	AbortCatchup(ctx echo.Context, catchpoint string) error
	// Starts a catchpoint catchup.
	// (POST /v2/catchup/{catchpoint})
	StartCatchup(ctx echo.Context, catchpoint string) error

	// (POST /v2/shutdown)
	ShutdownNode(ctx echo.Context, params ShutdownNodeParams) error
}

// ServerInterfaceWrapper converts echo contexts to parameters.
type ServerInterfaceWrapper struct {
	Handler ServerInterface
}

// AbortCatchup converts echo context to params.
func (w *ServerInterfaceWrapper) AbortCatchup(ctx echo.Context) error {
	var err error
	// ------------- Path parameter "catchpoint" -------------
	var catchpoint string

	err = runtime.BindStyledParameterWithLocation("simple", false, "catchpoint", runtime.ParamLocationPath, ctx.Param("catchpoint"), &catchpoint)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter catchpoint: %s", err))
	}

	ctx.Set(Api_keyScopes, []string{""})

	// Invoke the callback with all the unmarshalled arguments
	err = w.Handler.AbortCatchup(ctx, catchpoint)
	return err
}

// StartCatchup converts echo context to params.
func (w *ServerInterfaceWrapper) StartCatchup(ctx echo.Context) error {
	var err error
	// ------------- Path parameter "catchpoint" -------------
	var catchpoint string

	err = runtime.BindStyledParameterWithLocation("simple", false, "catchpoint", runtime.ParamLocationPath, ctx.Param("catchpoint"), &catchpoint)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter catchpoint: %s", err))
	}

	ctx.Set(Api_keyScopes, []string{""})

	// Invoke the callback with all the unmarshalled arguments
	err = w.Handler.StartCatchup(ctx, catchpoint)
	return err
}

// ShutdownNode converts echo context to params.
func (w *ServerInterfaceWrapper) ShutdownNode(ctx echo.Context) error {
	var err error

	ctx.Set(Api_keyScopes, []string{""})

	// Parameter object where we will unmarshal all parameters from the context
	var params ShutdownNodeParams
	// ------------- Optional query parameter "timeout" -------------

	err = runtime.BindQueryParameter("form", true, false, "timeout", ctx.QueryParams(), &params.Timeout)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter timeout: %s", err))
	}

	// Invoke the callback with all the unmarshalled arguments
	err = w.Handler.ShutdownNode(ctx, params)
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

	router.DELETE(baseURL+"/v2/catchup/:catchpoint", wrapper.AbortCatchup, m...)
	router.POST(baseURL+"/v2/catchup/:catchpoint", wrapper.StartCatchup, m...)
	router.POST(baseURL+"/v2/shutdown", wrapper.ShutdownNode, m...)

}

// Base64 encoded, gzipped, json marshaled Swagger object
var swaggerSpec = []string{

	"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",
	"//7jKXoargAA",
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
