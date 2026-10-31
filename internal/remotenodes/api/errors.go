// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// The codes the page keys its texts on. Each comes with an English sentence
// the page falls back on.
const (
	CodeBadRequest          = "bad_request"
	CodeForbidden           = "forbidden"
	CodeNotFound            = "not_found"
	CodeNodeLimit           = "node_limit"
	CodeSubscriptionLimit   = "subscription_limit"
	CodeNameTaken           = "name_taken"
	CodeEntryOff            = "entry_off"
	CodeNodeUnreachable     = "node_unreachable"
	CodeNeedsLogin          = "needs_login"
	CodeVersionMismatch     = "version_mismatch"
	CodeAgentTooOld         = "agent_too_old"
	CodeHostKeyChanged      = "host_key_changed"
	CodeServerHasXray       = "server_has_xray"  // an install found an Xray it did not put there
	CodeServerHasAgent      = "server_has_agent" // an install found another hub's agent, or one nobody vouches for
	CodeBusy                = "busy"
	CodeBackupBadPassphrase = "backup_bad_passphrase"
	CodeBackupBadFile       = "backup_bad_file"
	CodeInternal            = "internal"
)

var statuses = map[string]int{
	CodeBadRequest:          http.StatusBadRequest,
	CodeForbidden:           http.StatusForbidden,
	CodeNotFound:            http.StatusNotFound,
	CodeNodeLimit:           http.StatusConflict,
	CodeSubscriptionLimit:   http.StatusConflict,
	CodeNameTaken:           http.StatusConflict,
	CodeEntryOff:            http.StatusConflict,
	CodeNodeUnreachable:     http.StatusBadGateway,
	CodeNeedsLogin:          http.StatusConflict,
	CodeVersionMismatch:     http.StatusConflict,
	CodeAgentTooOld:         http.StatusConflict,
	CodeHostKeyChanged:      http.StatusConflict,
	CodeServerHasXray:       http.StatusConflict,
	CodeServerHasAgent:      http.StatusConflict,
	CodeBusy:                http.StatusConflict,
	CodeBackupBadPassphrase: http.StatusBadRequest,
	CodeBackupBadFile:       http.StatusBadRequest,
	CodeInternal:            http.StatusInternalServerError,
}

func statusFor(code string) int {
	if s, ok := statuses[code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// Error is a failure with one of the codes above.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// errorBody is every error answer.
type errorBody struct {
	Code      string `json:"code"`
	Error     string `json:"error"`
	NeedLogin bool   `json:"needLogin,omitempty"`
}

// classify gives a known error its code and sentence, and nil for anything
// else.
func classify(err error) *Error {
	var e *Error
	var inv *model.InvalidError
	switch {
	case errors.As(err, &e):
		return e
	case errors.Is(err, model.ErrNodeLimit):
		return &Error{CodeNodeLimit, fmt.Sprintf("This router runs at most %d nodes.", model.MaxNodes)}
	case errors.Is(err, model.ErrSubscriptionLimit):
		return &Error{CodeSubscriptionLimit, fmt.Sprintf("At most %d subscriptions can be added.", model.MaxSubscriptions)}
	case errors.Is(err, model.ErrNameTaken):
		return &Error{CodeNameTaken, "That name is already in use."}
	case errors.Is(err, model.ErrNotFound):
		return &Error{CodeNotFound, "There is no such node or subscription."}
	case errors.Is(err, jobs.ErrBusy):
		return busy(err)
	case errors.As(err, &inv):
		return &Error{CodeBadRequest, sentence(err.Error())}
	}
	return nil
}

// busy is the answer for a second job on a node. Handlers pass the error
// from Engine.Start to fail as it is, and it comes out as the busy code. The
// contract has no field for the running job's id, so it is only in the
// sentence.
func busy(err error) *Error {
	var b *jobs.BusyError
	if errors.As(err, &b) && b.JobID != "" {
		return &Error{CodeBusy, fmt.Sprintf("A job is already running on that node (job %s).", b.JobID)}
	}
	return &Error{CodeBusy, "A job is already running on that node."}
}

// sentence turns a message like "the name is required" into one that starts
// with a capital and ends with a full stop.
func sentence(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	s = string(unicode.ToUpper(r)) + s[n:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// fail answers err. What isn't a known error is the hub's own fault: it goes
// to the log and the page gets a plain sentence, never the error's text.
func (a *API) fail(w http.ResponseWriter, err error) {
	e := classify(err)
	if e == nil {
		a.cfg.Log.Error("request failed", "err", err)
		e = &Error{CodeInternal, "Something went wrong inside the hub. Its log has the details."}
	}
	writeJSON(w, statusFor(e.Code), errorBody{Code: e.Code, Error: e.Msg, NeedLogin: e.Code == CodeNeedsLogin})
}
