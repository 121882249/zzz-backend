//go:build integration

package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (s *APIKeyRepoSuite) TestListByUserID_SortByNameAsc() {
	user := s.mustCreateUser("sort-name@example.com")
	s.mustCreateApiKey(user.ID, "sk-z", "z-key", nil)
	s.mustCreateApiKey(user.ID, "sk-a", "a-key", nil)

	keys, _, err := s.repo.ListByUserID(s.ctx, user.ID, pagination.PaginationParams{
		Page:      1,
		PageSize:  10,
		SortBy:    "name",
		SortOrder: "asc",
	}, service.APIKeyListFilters{})
	s.Require().NoError(err)
	s.Require().Len(keys, 3)
	s.Require().Equal("a-key", keys[0].Name)
	s.Require().Equal("z-key", keys[1].Name)
	s.Require().Equal(service.APIKeyTypeGlobal, keys[2].KeyType)
}

func (s *APIKeyRepoSuite) TestListByUserID_SortByID() {
	user := s.mustCreateUser("sort-id@example.com")
	first := s.mustCreateApiKey(user.ID, "sk-id-a", "a-key", nil)
	second := s.mustCreateApiKey(user.ID, "sk-id-b", "b-key", nil)

	keys, _, err := s.repo.ListByUserID(s.ctx, user.ID, pagination.PaginationParams{
		Page:      1,
		PageSize:  10,
		SortBy:    "id",
		SortOrder: "desc",
	}, service.APIKeyListFilters{})
	s.Require().NoError(err)
	s.Require().Len(keys, 3)
	s.Require().Equal(second.ID, keys[0].ID)
	s.Require().Equal(first.ID, keys[1].ID)
	s.Require().Equal(service.APIKeyTypeGlobal, keys[2].KeyType)
}

func (s *APIKeyRepoSuite) TestListByUserID_GlobalKeyStaysOnLastPage() {
	user := s.mustCreateUser("sort-global-last-page@example.com")
	first := s.mustCreateApiKey(user.ID, "sk-page-a", "a-key", nil)
	second := s.mustCreateApiKey(user.ID, "sk-page-b", "b-key", nil)

	firstPage, result, err := s.repo.ListByUserID(s.ctx, user.ID, pagination.PaginationParams{
		Page:      1,
		PageSize:  2,
		SortBy:    "created_at",
		SortOrder: "desc",
	}, service.APIKeyListFilters{})
	s.Require().NoError(err)
	s.Require().Equal(int64(3), result.Total)
	s.Require().Len(firstPage, 2)
	s.Require().ElementsMatch([]int64{first.ID, second.ID}, []int64{firstPage[0].ID, firstPage[1].ID})

	lastPage, _, err := s.repo.ListByUserID(s.ctx, user.ID, pagination.PaginationParams{
		Page:      2,
		PageSize:  2,
		SortBy:    "created_at",
		SortOrder: "desc",
	}, service.APIKeyListFilters{})
	s.Require().NoError(err)
	s.Require().Len(lastPage, 1)
	s.Require().Equal(service.APIKeyTypeGlobal, lastPage[0].KeyType)
}
