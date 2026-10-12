package watch

import (
	"errors"

	"github.com/luevano/mangal/v5/storage"
)

// The JSON bodies of the `mangal serve` API (serve/), besides Watch, Report, CheckRun and
// Status.

// AddRequest is POST /api/watches, AddOptions in JSON.
type AddRequest struct {
	Offline   bool     `json:"offline,omitempty"`
	Title     string   `json:"title,omitempty"`
	Enabled   *bool    `json:"enabled,omitempty"`
	Provider  string   `json:"provider"`
	MangaID   string   `json:"manga_id,omitempty"`
	Query     string   `json:"query,omitempty"`
	Selector  string   `json:"selector,omitempty"`
	AnilistID int      `json:"anilist_id,omitempty"`
	From      *float32 `json:"from,omitempty"`
	Gaps      *bool    `json:"gaps,omitempty"`
	Schedule  string   `json:"schedule,omitempty"`
}

func (r AddRequest) Options() AddOptions {
	return AddOptions{Offline: r.Offline, Title: r.Title, Enabled: r.Enabled,
		Provider: r.Provider, MangaID: r.MangaID, Query: r.Query, Selector: r.Selector,
		AnilistID: r.AnilistID, From: r.From, Gaps: r.Gaps, Schedule: r.Schedule}
}

func addRequest(o AddOptions) AddRequest {
	return AddRequest{Offline: o.Offline, Title: o.Title, Enabled: o.Enabled,
		Provider: o.Provider, MangaID: o.MangaID, Query: o.Query, Selector: o.Selector,
		AnilistID: o.AnilistID, From: o.From, Gaps: o.Gaps, Schedule: o.Schedule}
}

// PatchRequest is PATCH /api/watches/{id}: Enabled pauses or resumes, the rest are
// UpdateOptions. Nil keeps the value.
type PatchRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
	// Schedule is a cron schedule, "" for watch.schedule.
	Schedule  *string  `json:"schedule,omitempty"`
	Gaps      *bool    `json:"gaps,omitempty"`
	AnilistID *int     `json:"anilist_id,omitempty"`
	From      *float32 `json:"from,omitempty"`
	// Skip replaces the chapter numbers never to download, [] clears them.
	Skip *[]float32 `json:"skip,omitempty"`
}

// Update are the UpdateOptions of the request, false when it changes none.
func (r PatchRequest) Update() (UpdateOptions, bool) {
	opts := UpdateOptions{Schedule: r.Schedule, Gaps: r.Gaps, AnilistID: r.AnilistID, From: r.From, Skip: r.Skip}
	return opts, opts != UpdateOptions{}
}

func patchRequest(o UpdateOptions) PatchRequest {
	return PatchRequest{Schedule: o.Schedule, Gaps: o.Gaps, AnilistID: o.AnilistID, From: o.From, Skip: o.Skip}
}

// CheckRequest is POST /api/check: the watches with the IDs (every enabled one if none).
// A dry run answers 200 with the Report, a check 202 with a CheckResponse (409 when every
// provider is being checked by another run) and runs in the server: GET /api/runs/{run} has it, finished once its
// finished time is set.
type CheckRequest struct {
	IDs    []int64 `json:"ids,omitempty"`
	Gaps   bool    `json:"gaps,omitempty"`
	DryRun bool    `json:"dry_run,omitempty"`
}

// CheckResponse is the check run a CheckRequest started, and the providers it skipped
// because another check has them.
type CheckResponse struct {
	Run  int64    `json:"run"`
	Busy []string `json:"busy,omitempty"`
}

// Error codes of an ErrorResponse, for the errors callers match.
const (
	CodeNotFound = "not_found"
	CodeExists   = "exists"
	CodeBusy     = "busy"
)

// ErrorResponse is the body of an API error.
type ErrorResponse struct {
	Error string `json:"error"`
	// Code is set for the errors that match ErrNotFound, ErrExists and ErrBusy.
	Code string `json:"code,omitempty"`
}

// ErrorCode is the Code of err, empty if none.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, storage.ErrRunNotFound):
		return CodeNotFound
	case errors.Is(err, ErrExists):
		return CodeExists
	case errors.Is(err, ErrBusy), errors.Is(err, storage.ErrRunUnfinished):
		return CodeBusy
	}
	return ""
}

// APIError is an error answered by the API, it matches the error of its Code.
type APIError struct {
	Status  int
	Message string
	Code    string
}

func (e *APIError) Error() string { return e.Message }

func (e *APIError) Is(target error) bool {
	switch e.Code {
	case CodeNotFound:
		return target == ErrNotFound
	case CodeExists:
		return target == ErrExists
	case CodeBusy:
		return target == ErrBusy
	}
	return false
}
