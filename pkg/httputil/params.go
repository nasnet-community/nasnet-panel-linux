package httputil

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ParamUint parses a uint path param (e.g. :id). On failure it writes a 400
// "invalid <name>" error and returns ok=false so the caller can just return.
func ParamUint(c *gin.Context, name string) (uint, bool) {
	v, err := strconv.ParseUint(c.Param(name), 10, 32)
	if err != nil {
		Error(c, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return uint(v), true
}
