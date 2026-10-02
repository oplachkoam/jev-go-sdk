package jev

import (
	"context"
	"net/http"

	"github.com/oplachkoam/jev-go-sdk/internal/apijson"
	"github.com/oplachkoam/jev-go-sdk/internal/requestconfig"
	"github.com/oplachkoam/jev-go-sdk/option"
	"github.com/oplachkoam/jev-go-sdk/packages/respjson"
)

// Model is the name of a model or of an alias that resolves to one, as
// accepted by [SystemOneNewParams].
type Model = string

const (
	// The most recent stable, official release. The default.
	ModelJevLatest Model = "jev-latest"
	// The most recent release, whether or not it is an official one.
	ModelJevPreview Model = "jev-preview"
	ModelJev1_13_0  Model = "jev-1.13.0"
)

// ModelService contains methods and other services that help with interacting with
// the TypeSafe API.
//
// Note, unlike clients, this service does not read variables from the environment
// automatically. You should not instantiate this service directly, and instead use
// the [NewModelService] method instead.
type ModelService struct {
	Options []option.RequestOption
}

// NewModelService generates a new service that applies the given options to each
// request. These options are applied after the parent client's options (if there
// is one), and before any request-specific options.
func NewModelService(opts ...option.RequestOption) (r ModelService) {
	r = ModelService{}
	r.Options = opts
	return
}

// Lists the names the account can send as the model of a request, with a
// description and release date for each. The list holds the aliases; versioned
// model IDs such as "jev-1.13.0" are accepted whether or not they are listed.
func (r *ModelService) List(ctx context.Context, opts ...option.RequestOption) (res *ModelListResponse, err error) {
	opts = append(r.Options[:len(r.Options):len(r.Options)], opts...)
	path := "v1/models"
	err = requestconfig.ExecuteNewRequest(ctx, http.MethodGet, path, nil, &res, opts...)
	return
}

type ModelListResponse struct {
	// One entry per model or alias.
	Models []ModelCard `json:"models,required"`
	// JSON contains metadata for fields, check presence with [respjson.Field.Valid].
	JSON struct {
		Models      respjson.Field
		ExtraFields map[string]respjson.Field
		raw         string
	} `json:"-"`
}

// Returns the unmodified JSON received from the API
func (r ModelListResponse) RawJSON() string { return r.JSON.raw }
func (r *ModelListResponse) UnmarshalJSON(data []byte) error {
	r.JSON.raw = string(data)
	return apijson.UnmarshalRoot(data, r)
}

// Metadata for an available model.
type ModelCard struct {
	// The model ID or alias, as accepted by the model field of a request.
	Name string `json:"name,required"`
	// What the model is for.
	Description string `json:"description,required"`
	// When the model or alias was released.
	ReleaseDate string `json:"release_date,required"`
	// JSON contains metadata for fields, check presence with [respjson.Field.Valid].
	JSON struct {
		Name        respjson.Field
		Description respjson.Field
		ReleaseDate respjson.Field
		ExtraFields map[string]respjson.Field
		raw         string
	} `json:"-"`
}

// Returns the unmodified JSON received from the API
func (r ModelCard) RawJSON() string { return r.JSON.raw }
func (r *ModelCard) UnmarshalJSON(data []byte) error {
	r.JSON.raw = string(data)
	return apijson.UnmarshalRoot(data, r)
}
