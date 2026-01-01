package httputil

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// MaxPerPage caps per_page to avoid unbounded queries.
const MaxPerPage = 100

// Page holds parsed pagination query params plus the derived DB offset.
type Page struct {
	Page    int
	PerPage int
	Offset  int
}

// ParsePage reads ?page and ?per_page, clamps them, and computes Offset.
// defaultPerPage is used when per_page is missing or out of [1, MaxPerPage].
func ParsePage(c *gin.Context, defaultPerPage int) Page {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", strconv.Itoa(defaultPerPage)))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > MaxPerPage {
		perPage = defaultPerPage
	}
	return Page{Page: page, PerPage: perPage, Offset: (page - 1) * perPage}
}

// Meta builds a pagination Meta for this page given a total row count.
func (p Page) Meta(total int) *Meta {
	totalPages := 0
	if p.PerPage > 0 {
		totalPages = (total + p.PerPage - 1) / p.PerPage
	}
	return &Meta{Page: p.Page, PerPage: p.PerPage, Total: total, TotalPages: totalPages}
}
