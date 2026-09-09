package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/admin/usecase"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
)

type paginationAdmin struct {
	usecase.AdminUsecase
	filter subRepo.SubscriptionFilter
}

func (a *paginationAdmin) ListAllFilteredSubscriptions(_ context.Context, filter subRepo.SubscriptionFilter) ([]*subDomain.Subscription, int64, error) {
	a.filter = filter
	return []*subDomain.Subscription{}, 50, nil
}

func TestSubscriptionsUsePagePagination(t *testing.T) {
	for _, tc := range []struct {
		query         string
		page, perPage int
	}{
		{"", 1, 20},
		{"?page=3&per_page=10", 3, 10},
		{"?offset=40&limit=5", 1, 20},
		{"?page=invalid&per_page=-1", 1, 20},
	} {
		t.Run(tc.query, func(t *testing.T) {
			admin := &paginationAdmin{}
			handler := &Handler{adminUsecase: admin}
			router := gin.New()
			router.GET("/subscriptions", handler.ListAllSubscriptions)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/subscriptions"+tc.query, nil))
			if response.Code != 200 || admin.filter.Offset != (tc.page-1)*tc.perPage || admin.filter.Limit != tc.perPage {
				t.Fatalf("pagination = %+v; response = %s", admin.filter, response.Body)
			}
			var body struct {
				Meta map[string]int `json:"meta"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Meta["page"] != tc.page || body.Meta["per_page"] != tc.perPage {
				t.Fatalf("pagination metadata = %v", body.Meta)
			}
			for _, key := range []string{"offset", "limit"} {
				if _, present := body.Meta[key]; present {
					t.Fatalf("obsolete pagination key %q in %v", key, body.Meta)
				}
			}
		})
	}
}
