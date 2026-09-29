package http

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	"gorm.io/gorm"
	"net/http"
	"strconv"
)

func RegisterPoolRoutes(rg *gin.RouterGroup, s *usecase.OutboundPoolService) {
	group := rg.Group("/nodes/:id/pools")
	fail := func(c *gin.Context, err error) {
		code := http.StatusBadRequest
		if errors.Is(err, gorm.ErrRecordNotFound) {
			code = 404
		}
		if errors.Is(err, usecase.ErrPoolBusy) {
			code = 409
		}
		c.JSON(code, gin.H{"success": false, "error": err.Error()})
	}
	ids := func(c *gin.Context) (uint, uint, bool) {
		node, e := strconv.ParseUint(c.Param("id"), 10, 32)
		if e != nil || node == 0 {
			c.JSON(400, gin.H{"success": false, "error": "invalid node ID"})
			return 0, 0, false
		}
		var pool uint64
		if c.Param("poolID") != "" {
			pool, e = strconv.ParseUint(c.Param("poolID"), 10, 32)
			if e != nil || pool == 0 {
				c.JSON(400, gin.H{"success": false, "error": "invalid pool ID"})
				return 0, 0, false
			}
		}
		return uint(node), uint(pool), true
	}
	group.GET("", func(c *gin.Context) {
		node, _, ok := ids(c)
		if !ok {
			return
		}
		data, err := s.List(c.Request.Context(), node)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, gin.H{"success": true, "data": data})
	})
	save := func(c *gin.Context) {
		node, pool, ok := ids(c)
		if !ok {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 70<<20)
		var input usecase.PoolInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"success": false, "error": "invalid or oversized pool settings"})
			return
		}
		data, err := s.Save(c.Request.Context(), node, pool, input)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, gin.H{"success": true, "data": data})
	}
	group.POST("", save)
	group.PUT("/:poolID", save)
	group.POST("/:poolID/run", func(c *gin.Context) {
		node, pool, ok := ids(c)
		if !ok {
			return
		}
		var input struct {
			Refresh bool `json:"refresh"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"success": false, "error": "invalid scan request"})
			return
		}
		data, err := s.Run(c.Request.Context(), node, pool, input.Refresh)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(202, gin.H{"success": true, "data": data})
	})
	group.POST("/:poolID/cancel", func(c *gin.Context) {
		node, pool, ok := ids(c)
		if !ok {
			return
		}
		if err := s.Cancel(c.Request.Context(), node, pool); err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, gin.H{"success": true})
	})
	group.DELETE("/:poolID", func(c *gin.Context) {
		node, pool, ok := ids(c)
		if !ok {
			return
		}
		if err := s.Delete(c.Request.Context(), node, pool); err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, gin.H{"success": true})
	})
	group.GET("/:poolID/candidates", func(c *gin.Context) {
		node, pool, ok := ids(c)
		if !ok {
			return
		}
		offset, _ := strconv.Atoi(c.Query("offset"))
		limit, _ := strconv.Atoi(c.Query("limit"))
		if offset < 0 {
			offset = 0
		}
		if limit < 1 || limit > 100 {
			limit = 50
		}
		rows, total, err := s.Candidates(c.Request.Context(), node, pool, offset, limit, c.Query("q"), c.Query("status"))
		if err != nil {
			fail(c, err)
			return
		}
		counts, err := s.CandidateCounts(c.Request.Context(), node, pool, c.Query("q"))
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, gin.H{"success": true, "data": gin.H{"items": rows, "total": total, "counts": counts}})
	})
	group.GET("/:poolID/runs", func(c *gin.Context) {
		node, pool, ok := ids(c)
		if !ok {
			return
		}
		data, err := s.Runs(c.Request.Context(), node, pool)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, gin.H{"success": true, "data": data})
	})
	group.PUT("/:poolID/candidates/:candidateID", func(c *gin.Context) {
		node, pool, ok := ids(c)
		if !ok {
			return
		}
		id, err := strconv.ParseUint(c.Param("candidateID"), 10, 32)
		if err != nil {
			c.JSON(400, gin.H{"success": false, "error": "invalid candidate ID"})
			return
		}
		var input struct {
			Excluded bool `json:"excluded"`
		}
		if c.ShouldBindJSON(&input) != nil {
			c.JSON(400, gin.H{"success": false, "error": "invalid exclusion"})
			return
		}
		if err = s.Exclude(c.Request.Context(), node, pool, uint(id), input.Excluded); err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, gin.H{"success": true})
	})
}
