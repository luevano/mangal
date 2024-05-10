// Package api provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/deepmap/oapi-codegen/v2 version v2.0.0 DO NOT EDIT.
package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/oapi-codegen/runtime"
	strictecho "github.com/oapi-codegen/runtime/strictmiddleware/echo"
)

// ServerInterface represents all server handlers.
type ServerInterface interface {

	// (GET /chapter)
	GetChapter(ctx echo.Context, params GetChapterParams) error

	// (GET /formats)
	GetFormats(ctx echo.Context) error

	// (GET /image)
	GetImage(ctx echo.Context, params GetImageParams) error

	// (GET /manga)
	GetManga(ctx echo.Context, params GetMangaParams) error

	// (GET /mangaPage)
	GetMangaPage(ctx echo.Context, params GetMangaPageParams) error

	// (GET /mangaVolumes)
	GetMangaVolumes(ctx echo.Context, params GetMangaVolumesParams) error

	// (GET /mangalInfo)
	GetMangalInfo(ctx echo.Context) error

	// (GET /provider)
	GetProvider(ctx echo.Context, params GetProviderParams) error

	// (GET /providers)
	GetProviders(ctx echo.Context) error

	// (GET /searchMangas)
	SearchMangas(ctx echo.Context, params SearchMangasParams) error

	// (GET /volumeChapters)
	GetVolumeChapters(ctx echo.Context, params GetVolumeChaptersParams) error
}

// ServerInterfaceWrapper converts echo contexts to parameters.
type ServerInterfaceWrapper struct {
	Handler ServerInterface
}

// GetChapter converts echo context to params.
func (w *ServerInterfaceWrapper) GetChapter(ctx echo.Context) error {
	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params GetChapterParams
	// ------------- Required query parameter "provider" -------------

	err = runtime.BindQueryParameter("form", true, true, "provider", ctx.QueryParams(), &params.Provider)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter provider: %s", err))
	}

	// ------------- Required query parameter "query" -------------

	err = runtime.BindQueryParameter("form", true, true, "query", ctx.QueryParams(), &params.Query)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter query: %s", err))
	}

	// ------------- Required query parameter "manga" -------------

	err = runtime.BindQueryParameter("form", true, true, "manga", ctx.QueryParams(), &params.Manga)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter manga: %s", err))
	}

	// ------------- Required query parameter "volume" -------------

	err = runtime.BindQueryParameter("form", true, true, "volume", ctx.QueryParams(), &params.Volume)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter volume: %s", err))
	}

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetChapter(ctx, params)
	return err
}

// GetFormats converts echo context to params.
func (w *ServerInterfaceWrapper) GetFormats(ctx echo.Context) error {
	var err error

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetFormats(ctx)
	return err
}

// GetImage converts echo context to params.
func (w *ServerInterfaceWrapper) GetImage(ctx echo.Context) error {
	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params GetImageParams
	// ------------- Required query parameter "url" -------------

	err = runtime.BindQueryParameter("form", true, true, "url", ctx.QueryParams(), &params.Url)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter url: %s", err))
	}

	// ------------- Optional query parameter "referer" -------------

	err = runtime.BindQueryParameter("form", true, false, "referer", ctx.QueryParams(), &params.Referer)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter referer: %s", err))
	}

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetImage(ctx, params)
	return err
}

// GetManga converts echo context to params.
func (w *ServerInterfaceWrapper) GetManga(ctx echo.Context) error {
	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params GetMangaParams
	// ------------- Required query parameter "provider" -------------

	err = runtime.BindQueryParameter("form", true, true, "provider", ctx.QueryParams(), &params.Provider)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter provider: %s", err))
	}

	// ------------- Required query parameter "query" -------------

	err = runtime.BindQueryParameter("form", true, true, "query", ctx.QueryParams(), &params.Query)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter query: %s", err))
	}

	// ------------- Required query parameter "manga" -------------

	err = runtime.BindQueryParameter("form", true, true, "manga", ctx.QueryParams(), &params.Manga)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter manga: %s", err))
	}

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetManga(ctx, params)
	return err
}

// GetMangaPage converts echo context to params.
func (w *ServerInterfaceWrapper) GetMangaPage(ctx echo.Context) error {
	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params GetMangaPageParams
	// ------------- Required query parameter "provider" -------------

	err = runtime.BindQueryParameter("form", true, true, "provider", ctx.QueryParams(), &params.Provider)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter provider: %s", err))
	}

	// ------------- Required query parameter "query" -------------

	err = runtime.BindQueryParameter("form", true, true, "query", ctx.QueryParams(), &params.Query)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter query: %s", err))
	}

	// ------------- Required query parameter "manga" -------------

	err = runtime.BindQueryParameter("form", true, true, "manga", ctx.QueryParams(), &params.Manga)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter manga: %s", err))
	}

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetMangaPage(ctx, params)
	return err
}

// GetMangaVolumes converts echo context to params.
func (w *ServerInterfaceWrapper) GetMangaVolumes(ctx echo.Context) error {
	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params GetMangaVolumesParams
	// ------------- Required query parameter "provider" -------------

	err = runtime.BindQueryParameter("form", true, true, "provider", ctx.QueryParams(), &params.Provider)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter provider: %s", err))
	}

	// ------------- Required query parameter "query" -------------

	err = runtime.BindQueryParameter("form", true, true, "query", ctx.QueryParams(), &params.Query)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter query: %s", err))
	}

	// ------------- Required query parameter "manga" -------------

	err = runtime.BindQueryParameter("form", true, true, "manga", ctx.QueryParams(), &params.Manga)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter manga: %s", err))
	}

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetMangaVolumes(ctx, params)
	return err
}

// GetMangalInfo converts echo context to params.
func (w *ServerInterfaceWrapper) GetMangalInfo(ctx echo.Context) error {
	var err error

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetMangalInfo(ctx)
	return err
}

// GetProvider converts echo context to params.
func (w *ServerInterfaceWrapper) GetProvider(ctx echo.Context) error {
	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params GetProviderParams
	// ------------- Required query parameter "id" -------------

	err = runtime.BindQueryParameter("form", true, true, "id", ctx.QueryParams(), &params.Id)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter id: %s", err))
	}

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetProvider(ctx, params)
	return err
}

// GetProviders converts echo context to params.
func (w *ServerInterfaceWrapper) GetProviders(ctx echo.Context) error {
	var err error

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetProviders(ctx)
	return err
}

// SearchMangas converts echo context to params.
func (w *ServerInterfaceWrapper) SearchMangas(ctx echo.Context) error {
	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params SearchMangasParams
	// ------------- Required query parameter "provider" -------------

	err = runtime.BindQueryParameter("form", true, true, "provider", ctx.QueryParams(), &params.Provider)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter provider: %s", err))
	}

	// ------------- Required query parameter "query" -------------

	err = runtime.BindQueryParameter("form", true, true, "query", ctx.QueryParams(), &params.Query)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter query: %s", err))
	}

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.SearchMangas(ctx, params)
	return err
}

// GetVolumeChapters converts echo context to params.
func (w *ServerInterfaceWrapper) GetVolumeChapters(ctx echo.Context) error {
	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params GetVolumeChaptersParams
	// ------------- Required query parameter "provider" -------------

	err = runtime.BindQueryParameter("form", true, true, "provider", ctx.QueryParams(), &params.Provider)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter provider: %s", err))
	}

	// ------------- Required query parameter "query" -------------

	err = runtime.BindQueryParameter("form", true, true, "query", ctx.QueryParams(), &params.Query)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter query: %s", err))
	}

	// ------------- Required query parameter "manga" -------------

	err = runtime.BindQueryParameter("form", true, true, "manga", ctx.QueryParams(), &params.Manga)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter manga: %s", err))
	}

	// ------------- Required query parameter "volume" -------------

	err = runtime.BindQueryParameter("form", true, true, "volume", ctx.QueryParams(), &params.Volume)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid format for parameter volume: %s", err))
	}

	// Invoke the callback with all the unmarshaled arguments
	err = w.Handler.GetVolumeChapters(ctx, params)
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
func RegisterHandlers(router EchoRouter, si ServerInterface) {
	RegisterHandlersWithBaseURL(router, si, "")
}

// Registers handlers, and prepends BaseURL to the paths, so that the paths
// can be served under a prefix.
func RegisterHandlersWithBaseURL(router EchoRouter, si ServerInterface, baseURL string) {

	wrapper := ServerInterfaceWrapper{
		Handler: si,
	}

	router.GET(baseURL+"/chapter", wrapper.GetChapter)
	router.GET(baseURL+"/formats", wrapper.GetFormats)
	router.GET(baseURL+"/image", wrapper.GetImage)
	router.GET(baseURL+"/manga", wrapper.GetManga)
	router.GET(baseURL+"/mangaPage", wrapper.GetMangaPage)
	router.GET(baseURL+"/mangaVolumes", wrapper.GetMangaVolumes)
	router.GET(baseURL+"/mangalInfo", wrapper.GetMangalInfo)
	router.GET(baseURL+"/provider", wrapper.GetProvider)
	router.GET(baseURL+"/providers", wrapper.GetProviders)
	router.GET(baseURL+"/searchMangas", wrapper.SearchMangas)
	router.GET(baseURL+"/volumeChapters", wrapper.GetVolumeChapters)

}

type GetChapterRequestObject struct {
	Params GetChapterParams
}

type GetChapterResponseObject interface {
	VisitGetChapterResponse(w http.ResponseWriter) error
}

type GetChapter200JSONResponse Chapter

func (response GetChapter200JSONResponse) VisitGetChapterResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetFormatsRequestObject struct {
}

type GetFormatsResponseObject interface {
	VisitGetFormatsResponse(w http.ResponseWriter) error
}

type GetFormats200JSONResponse []Format

func (response GetFormats200JSONResponse) VisitGetFormatsResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetImageRequestObject struct {
	Params GetImageParams
}

type GetImageResponseObject interface {
	VisitGetImageResponse(w http.ResponseWriter) error
}

type GetImage200ImagepngResponse struct {
	Body          io.Reader
	ContentLength int64
}

func (response GetImage200ImagepngResponse) VisitGetImageResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "image/png")
	if response.ContentLength != 0 {
		w.Header().Set("Content-Length", fmt.Sprint(response.ContentLength))
	}
	w.WriteHeader(200)

	if closer, ok := response.Body.(io.ReadCloser); ok {
		defer closer.Close()
	}
	_, err := io.Copy(w, response.Body)
	return err
}

type GetMangaRequestObject struct {
	Params GetMangaParams
}

type GetMangaResponseObject interface {
	VisitGetMangaResponse(w http.ResponseWriter) error
}

type GetManga200JSONResponse Manga

func (response GetManga200JSONResponse) VisitGetMangaResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetManga404Response struct {
}

func (response GetManga404Response) VisitGetMangaResponse(w http.ResponseWriter) error {
	w.WriteHeader(404)
	return nil
}

type GetMangadefaultJSONResponse struct {
	Body       Error
	StatusCode int
}

func (response GetMangadefaultJSONResponse) VisitGetMangaResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)

	return json.NewEncoder(w).Encode(response.Body)
}

type GetMangaPageRequestObject struct {
	Params GetMangaPageParams
}

type GetMangaPageResponseObject interface {
	VisitGetMangaPageResponse(w http.ResponseWriter) error
}

type GetMangaPage200JSONResponse MangaPage

func (response GetMangaPage200JSONResponse) VisitGetMangaPageResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetMangaPagedefaultJSONResponse struct {
	Body       Error
	StatusCode int
}

func (response GetMangaPagedefaultJSONResponse) VisitGetMangaPageResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)

	return json.NewEncoder(w).Encode(response.Body)
}

type GetMangaVolumesRequestObject struct {
	Params GetMangaVolumesParams
}

type GetMangaVolumesResponseObject interface {
	VisitGetMangaVolumesResponse(w http.ResponseWriter) error
}

type GetMangaVolumes200JSONResponse []Volume

func (response GetMangaVolumes200JSONResponse) VisitGetMangaVolumesResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetMangaVolumesdefaultJSONResponse struct {
	Body       Error
	StatusCode int
}

func (response GetMangaVolumesdefaultJSONResponse) VisitGetMangaVolumesResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)

	return json.NewEncoder(w).Encode(response.Body)
}

type GetMangalInfoRequestObject struct {
}

type GetMangalInfoResponseObject interface {
	VisitGetMangalInfoResponse(w http.ResponseWriter) error
}

type GetMangalInfo200JSONResponse MangalInfo

func (response GetMangalInfo200JSONResponse) VisitGetMangalInfoResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetMangalInfodefaultJSONResponse struct {
	Body       Error
	StatusCode int
}

func (response GetMangalInfodefaultJSONResponse) VisitGetMangalInfoResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)

	return json.NewEncoder(w).Encode(response.Body)
}

type GetProviderRequestObject struct {
	Params GetProviderParams
}

type GetProviderResponseObject interface {
	VisitGetProviderResponse(w http.ResponseWriter) error
}

type GetProvider200JSONResponse Provider

func (response GetProvider200JSONResponse) VisitGetProviderResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetProvider404Response struct {
}

func (response GetProvider404Response) VisitGetProviderResponse(w http.ResponseWriter) error {
	w.WriteHeader(404)
	return nil
}

type GetProvidersRequestObject struct {
}

type GetProvidersResponseObject interface {
	VisitGetProvidersResponse(w http.ResponseWriter) error
}

type GetProviders200JSONResponse []Provider

func (response GetProviders200JSONResponse) VisitGetProvidersResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetProvidersdefaultJSONResponse struct {
	Body       Error
	StatusCode int
}

func (response GetProvidersdefaultJSONResponse) VisitGetProvidersResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)

	return json.NewEncoder(w).Encode(response.Body)
}

type SearchMangasRequestObject struct {
	Params SearchMangasParams
}

type SearchMangasResponseObject interface {
	VisitSearchMangasResponse(w http.ResponseWriter) error
}

type SearchMangas200JSONResponse []Manga

func (response SearchMangas200JSONResponse) VisitSearchMangasResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type SearchMangasdefaultJSONResponse struct {
	Body       Error
	StatusCode int
}

func (response SearchMangasdefaultJSONResponse) VisitSearchMangasResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)

	return json.NewEncoder(w).Encode(response.Body)
}

type GetVolumeChaptersRequestObject struct {
	Params GetVolumeChaptersParams
}

type GetVolumeChaptersResponseObject interface {
	VisitGetVolumeChaptersResponse(w http.ResponseWriter) error
}

type GetVolumeChapters200JSONResponse []Chapter

func (response GetVolumeChapters200JSONResponse) VisitGetVolumeChaptersResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetVolumeChaptersdefaultJSONResponse struct {
	Body       Error
	StatusCode int
}

func (response GetVolumeChaptersdefaultJSONResponse) VisitGetVolumeChaptersResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)

	return json.NewEncoder(w).Encode(response.Body)
}

// StrictServerInterface represents all server handlers.
type StrictServerInterface interface {

	// (GET /chapter)
	GetChapter(ctx context.Context, request GetChapterRequestObject) (GetChapterResponseObject, error)

	// (GET /formats)
	GetFormats(ctx context.Context, request GetFormatsRequestObject) (GetFormatsResponseObject, error)

	// (GET /image)
	GetImage(ctx context.Context, request GetImageRequestObject) (GetImageResponseObject, error)

	// (GET /manga)
	GetManga(ctx context.Context, request GetMangaRequestObject) (GetMangaResponseObject, error)

	// (GET /mangaPage)
	GetMangaPage(ctx context.Context, request GetMangaPageRequestObject) (GetMangaPageResponseObject, error)

	// (GET /mangaVolumes)
	GetMangaVolumes(ctx context.Context, request GetMangaVolumesRequestObject) (GetMangaVolumesResponseObject, error)

	// (GET /mangalInfo)
	GetMangalInfo(ctx context.Context, request GetMangalInfoRequestObject) (GetMangalInfoResponseObject, error)

	// (GET /provider)
	GetProvider(ctx context.Context, request GetProviderRequestObject) (GetProviderResponseObject, error)

	// (GET /providers)
	GetProviders(ctx context.Context, request GetProvidersRequestObject) (GetProvidersResponseObject, error)

	// (GET /searchMangas)
	SearchMangas(ctx context.Context, request SearchMangasRequestObject) (SearchMangasResponseObject, error)

	// (GET /volumeChapters)
	GetVolumeChapters(ctx context.Context, request GetVolumeChaptersRequestObject) (GetVolumeChaptersResponseObject, error)
}

type StrictHandlerFunc = strictecho.StrictEchoHandlerFunc
type StrictMiddlewareFunc = strictecho.StrictEchoMiddlewareFunc

func NewStrictHandler(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc) ServerInterface {
	return &strictHandler{ssi: ssi, middlewares: middlewares}
}

type strictHandler struct {
	ssi         StrictServerInterface
	middlewares []StrictMiddlewareFunc
}

// GetChapter operation middleware
func (sh *strictHandler) GetChapter(ctx echo.Context, params GetChapterParams) error {
	var request GetChapterRequestObject

	request.Params = params

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetChapter(ctx.Request().Context(), request.(GetChapterRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetChapter")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetChapterResponseObject); ok {
		return validResponse.VisitGetChapterResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// GetFormats operation middleware
func (sh *strictHandler) GetFormats(ctx echo.Context) error {
	var request GetFormatsRequestObject

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetFormats(ctx.Request().Context(), request.(GetFormatsRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetFormats")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetFormatsResponseObject); ok {
		return validResponse.VisitGetFormatsResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// GetImage operation middleware
func (sh *strictHandler) GetImage(ctx echo.Context, params GetImageParams) error {
	var request GetImageRequestObject

	request.Params = params

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetImage(ctx.Request().Context(), request.(GetImageRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetImage")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetImageResponseObject); ok {
		return validResponse.VisitGetImageResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// GetManga operation middleware
func (sh *strictHandler) GetManga(ctx echo.Context, params GetMangaParams) error {
	var request GetMangaRequestObject

	request.Params = params

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetManga(ctx.Request().Context(), request.(GetMangaRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetManga")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetMangaResponseObject); ok {
		return validResponse.VisitGetMangaResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// GetMangaPage operation middleware
func (sh *strictHandler) GetMangaPage(ctx echo.Context, params GetMangaPageParams) error {
	var request GetMangaPageRequestObject

	request.Params = params

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetMangaPage(ctx.Request().Context(), request.(GetMangaPageRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetMangaPage")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetMangaPageResponseObject); ok {
		return validResponse.VisitGetMangaPageResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// GetMangaVolumes operation middleware
func (sh *strictHandler) GetMangaVolumes(ctx echo.Context, params GetMangaVolumesParams) error {
	var request GetMangaVolumesRequestObject

	request.Params = params

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetMangaVolumes(ctx.Request().Context(), request.(GetMangaVolumesRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetMangaVolumes")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetMangaVolumesResponseObject); ok {
		return validResponse.VisitGetMangaVolumesResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// GetMangalInfo operation middleware
func (sh *strictHandler) GetMangalInfo(ctx echo.Context) error {
	var request GetMangalInfoRequestObject

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetMangalInfo(ctx.Request().Context(), request.(GetMangalInfoRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetMangalInfo")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetMangalInfoResponseObject); ok {
		return validResponse.VisitGetMangalInfoResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// GetProvider operation middleware
func (sh *strictHandler) GetProvider(ctx echo.Context, params GetProviderParams) error {
	var request GetProviderRequestObject

	request.Params = params

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetProvider(ctx.Request().Context(), request.(GetProviderRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetProvider")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetProviderResponseObject); ok {
		return validResponse.VisitGetProviderResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// GetProviders operation middleware
func (sh *strictHandler) GetProviders(ctx echo.Context) error {
	var request GetProvidersRequestObject

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetProviders(ctx.Request().Context(), request.(GetProvidersRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetProviders")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetProvidersResponseObject); ok {
		return validResponse.VisitGetProvidersResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// SearchMangas operation middleware
func (sh *strictHandler) SearchMangas(ctx echo.Context, params SearchMangasParams) error {
	var request SearchMangasRequestObject

	request.Params = params

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.SearchMangas(ctx.Request().Context(), request.(SearchMangasRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "SearchMangas")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(SearchMangasResponseObject); ok {
		return validResponse.VisitSearchMangasResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// GetVolumeChapters operation middleware
func (sh *strictHandler) GetVolumeChapters(ctx echo.Context, params GetVolumeChaptersParams) error {
	var request GetVolumeChaptersRequestObject

	request.Params = params

	handler := func(ctx echo.Context, request interface{}) (interface{}, error) {
		return sh.ssi.GetVolumeChapters(ctx.Request().Context(), request.(GetVolumeChaptersRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetVolumeChapters")
	}

	response, err := handler(ctx, request)

	if err != nil {
		return err
	} else if validResponse, ok := response.(GetVolumeChaptersResponseObject); ok {
		return validResponse.VisitGetVolumeChaptersResponse(ctx.Response())
	} else if response != nil {
		return fmt.Errorf("unexpected response type: %T", response)
	}
	return nil
}

// Base64 encoded, gzipped, json marshaled Swagger object
var swaggerSpec = []string{

	"H4sIAAAAAAAC/+xY0Y/bqBP+VxC/36PV5No+5e1u1Z5WanVRq24fen0g9iShwkABpxtV+d9PBoydALGr",
	"bHuRLk+78Yz5ZoZvPgZ/x6WopeDAjcaL71iXW6iJ/fd3ThnV5i3hG9L+lkpIUIaCta4I56Dua7KB9qfZ",
	"S8ALrI2ifIMPBS7FbmD+v4I1XuD/zXq0mYea3fWehwJXoEtFpaGCJ9Y9FFjB14YqqPDi0xDkc9H5itUX",
	"KE271t2WSAMqDp439co9XwtVE4MXeM0EMbjANeW0bmq8mIcFvfehwIYalk63UWw8XPd60S2YDPmobMdR",
	"l4IJlUSHR6PIG6Iye8Gylhoq2iY7FvkAoFsuvFz4wFLpvFJKqFQmFRxVn3Lz4jkOC1BuYONKXoPWaY5F",
	"XKhcUM4/Fc1rD3caDjwa4DrNuAJzUk+At14p0LCZIdkV5UTt+2x7qLO9lm+zpIVWycdPRGFaJZO1CSyT",
	"7CUncnJOEo6kpyXBlJeC906wpnag1ECtx158sP4fqdl6wdC2UC43ohTZR2VwAfVQ2Vqwe74WcTF2oPQk",
	"hescUwBLJXa0SunbeRHNciPD9GJ6uLTCvXeRbwpX8h8S5hMxPm2/vKQmtjcWpIFlEme6oyUiSseJaaSL",
	"99s9LvqI4pTad6inlW9nTzb08dUf6MP9YA8W+Ldn82fzNi4hgRNJ8QK/sI8KLInZ2kxnZX9SbsBEFMJ/",
	"gkGdj11JkdZyXznbXTBJokgNrpSfTleRnrCIVsgI1GirJK3lawNWER0DgyMeFseoBgo/oCSZeApnmxRp",
	"IKrcog4hBdf9vBjL0j+F0OnFBQiOGcgTPQ0T2JPHGe2rz+3bWgquXWs8n8/dkc0NcEsNIiWjpSXA7It2",
	"stADTOqaw+mk1/Eddditz6HAMxevPstMsiOUrBigzjlB0dfBdFF2k8TBzxnxIRJl7QOOsqbd0JDN2Xqg",
	"tRI1+vDuDVrtJdGa8g26++vdeyQFo+U+VQc3jow0qlu8Uaxt00p840yQHLXbseEiYitYgwLlFaH9swGD",
	"zBZcjhlY/xY+BzVOZYswk3wzncP+phLvpSva6U6G0SW7k502RFv11htumvpEmvozpc1PnzEtXBY9LQr8",
	"cv4yZoJz48KgtWh45a7Ca9Iw82QhustYIsSGw6OE0kCFoPMJ3F2OKZGLXLbk/0bNFvmZGBFeoTDF5Oi9",
	"nKBGN4pfEcWXGfUbsGDI9evg8EN/I0zSeBNo3F3ocnx9CPYbZa+Esj9wx58ykh3z4GooHD4k5CdC7qZJ",
	"KjgiK9G0T7QhjEGF/PWQSJlltgP42fLhUHJlZzaJ6xIQOfjKkq39oI2j6i5721TNyPSWNVzHWRCSSlQy",
	"JDI69ATPwdxzXPWJF7/g/jc/twW/5vrX12ZcbULg10V6d5TYjs3vgD9v1kK5wzM+Nd8Pl/nPHJm/5EAL",
	"X7vHGOZTVKAbZq7jQHOH6/BrbHYq81+9zt0jHo5Xu01mt2+Ql33Cj4l8ysJ/vYkOh38CAAD//45ReEa5",
	"HwAA",
}

// GetSwagger returns the content of the embedded swagger specification file
// or error if failed to decode
func decodeSpec() ([]byte, error) {
	zipped, err := base64.StdEncoding.DecodeString(strings.Join(swaggerSpec, ""))
	if err != nil {
		return nil, fmt.Errorf("error base64 decoding spec: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %w", err)
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(zr)
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %w", err)
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
	res := make(map[string]func() ([]byte, error))
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
	resolvePath := PathToRawSpec("")

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.ReadFromURIFunc = func(loader *openapi3.Loader, url *url.URL) ([]byte, error) {
		pathToFile := url.String()
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
