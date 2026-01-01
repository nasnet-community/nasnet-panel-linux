package httputil

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BindJSON binds the request body into v. On failure it writes a 400 with the
// bind error and returns false so the caller can just return.
func BindJSON(c *gin.Context, v interface{}) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		Error(c, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}
