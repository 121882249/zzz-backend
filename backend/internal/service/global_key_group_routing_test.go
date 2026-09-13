//go:build unit

package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type globalRouteAccountRepo struct {
	*mockAccountRepoForPlatform
	byGroup map[int64][]Account
}

func (r *globalRouteAccountRepo) ListSchedulableByGroupID(_ context.Context, groupID int64) ([]Account, error) {
	return append([]Account(nil), r.byGroup[groupID]...), nil
}

func (r *globalRouteAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]Account, error) {
	var out []Account
	for _, account := range r.byGroup[groupID] {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out, nil
}

func (r *globalRouteAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for _, accounts := range r.byGroup {
		for i := range accounts {
			if accounts[i].ID == id {
				account := accounts[i]
				return &account, nil
			}
		}
	}
	return nil, fmt.Errorf("account %d not found", id)
}

type globalRouteGroupRepo struct {
	*mockGroupRepoForGateway
	active []Group
}

func (r *globalRouteGroupRepo) ListActive(context.Context) ([]Group, error) {
	return append([]Group(nil), r.active...), nil
}

func (r *globalRouteGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	if group, ok := r.groups[id]; ok {
		return group, nil
	}
	return nil, ErrGroupNotFound
}

func (r *globalRouteGroupRepo) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	return r.GetByID(ctx, id)
}

func TestResolveGlobalGroupForModelHonorsAllowlistAndPreferredGroup(t *testing.T) {
	groups := []Group{
		{ID: 16, Name: "text", Platform: PlatformOpenAI, Status: StatusActive,
			ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.6-sol"}}},
		{ID: 65, Name: "images", Platform: PlatformOpenAI, Status: StatusActive,
			ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-image-2.5-*"}}},
	}
	account := func(id int64, models map[string]any) Account {
		return Account{ID: id, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": models}}
	}
	accounts := &globalRouteAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{}},
		byGroup: map[int64][]Account{
			16: {account(160, map[string]any{"gpt-5.6-sol": "gpt-5.6-sol", "gpt-image-2.5-sunburst": "gpt-image-2.5-sunburst"})},
			65: {account(650, map[string]any{"gpt-image-2.5-sunburst": "gpt-image-2.5-sunburst"})},
		},
	}
	groupMap := map[int64]*Group{16: &groups[0], 65: &groups[1]}
	groupRepo := &globalRouteGroupRepo{
		mockGroupRepoForGateway: &mockGroupRepoForGateway{groups: groupMap},
		active:                  groups,
	}
	svc := &GatewayService{accountRepo: accounts, groupRepo: groupRepo, cfg: testConfig()}

	_, err := svc.ResolveGlobalGroupForModelWithUserAndGroup(
		context.Background(), nil, 18, "", "gpt-image-2.5-sunburst", nil, nil,
	)
	require.ErrorIs(t, err, ErrGlobalGroupRequired,
		"a global key must never scan groups when the client omitted group_id")

	preferred := int64(65)
	resolved, err := svc.ResolveGlobalGroupForModelWithUserAndGroup(
		context.Background(), nil, 18, "", "gpt-image-2.5-sunburst", &preferred, nil,
	)
	require.NoError(t, err)
	require.Equal(t, preferred, resolved.Group.ID)

	wrong := int64(16)
	_, err = svc.ResolveGlobalGroupForModelWithUserAndGroup(
		context.Background(), nil, 18, "", "gpt-image-2.5-sunburst", &wrong, nil,
	)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
}

func TestResolveGlobalGroupForModelConcurrentPreferredGroups(t *testing.T) {
	groups := []Group{
		{ID: 16, Name: "text", Platform: PlatformOpenAI, Status: StatusActive,
			ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.6-sol"}}},
		{ID: 65, Name: "images", Platform: PlatformOpenAI, Status: StatusActive,
			ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-image-2.5-sunburst"}}},
	}
	account := func(id int64, model string) Account {
		return Account{ID: id, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{model: model}}}
	}
	accounts := &globalRouteAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{}},
		byGroup: map[int64][]Account{
			16: {account(160, "gpt-5.6-sol")},
			65: {account(650, "gpt-image-2.5-sunburst")},
		},
	}
	groupRepo := &globalRouteGroupRepo{
		mockGroupRepoForGateway: &mockGroupRepoForGateway{groups: map[int64]*Group{16: &groups[0], 65: &groups[1]}},
		active:                  groups,
	}
	svc := &GatewayService{accountRepo: accounts, groupRepo: groupRepo, cfg: testConfig()}

	type route struct {
		groupID int64
		model   string
	}
	routes := []route{{16, "gpt-5.6-sol"}, {65, "gpt-image-2.5-sunburst"}}
	errors := make(chan error, 80)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		for _, request := range routes {
			request := request
			wg.Add(1)
			go func() {
				defer wg.Done()
				preferred := request.groupID
				resolved, err := svc.ResolveGlobalGroupForModelWithUserAndGroup(
					context.Background(), nil, 18, "", request.model, &preferred, nil,
				)
				if err != nil {
					errors <- err
					return
				}
				if resolved.Group.ID != request.groupID {
					errors <- fmt.Errorf("model %s resolved group %d, want %d", request.model, resolved.Group.ID, request.groupID)
				}
			}()
		}
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
}
