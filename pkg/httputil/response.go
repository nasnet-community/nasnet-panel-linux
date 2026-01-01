// Package httputil holds shared helpers for the gin HTTP delivery layer:
// a single response envelope plus request/param/pagination parsing. It exists
// to replace the ~1300 hand-rolled c.JSON(...) envelopes scattered across
// internal/*/delivery/http handlers with one consistent shape.
package httputil

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response is the standard API envelope. Data/Error/Meta are omitted when empty
// so success and failure bodies stay minimal.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

// Meta carries pagination info. Fields are always serialized (no omitempty) to
// keep the shape stable for clients.
type Meta struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// Error writes a failure envelope with the given status code.
func Error(c *gin.Context, status int, msg string) {
	c.JSON(status, Response{Success: false, Error: msg})
}

// JSON writes a success envelope with the given status code and data.
func JSON(c *gin.Context, status int, data interface{}) {
	c.JSON(status, Response{Success: true, Data: data})
}

// OK writes a 200 success envelope.
func OK(c *gin.Context, data interface{}) { JSON(c, http.StatusOK, data) }

// Created writes a 201 success envelope.
func Created(c *gin.Context, data interface{}) { JSON(c, http.StatusCreated, data) }

// Paged writes a 200 success envelope with pagination meta.
func Paged(c *gin.Context, data interface{}, meta *Meta) {
	c.JSON(http.StatusOK, Response{Success: true, Data: data, Meta: meta})
}
