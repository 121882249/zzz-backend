//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// imageLaneRepo records every billing command that reaches the per-user lane.
// Apply blocks on gate[requestID] so a later RecordUsage can queue behind it.
type imageLaneRepo struct {
	mu       sync.Mutex
	gate     map[string]chan struct{}
	started  chan string
	commands []*UsageBillingCommand
	applies  []string
	batches  [][]string
}

func newImageLaneRepo() *imageLaneRepo {
	return &imageLaneRepo{
		gate:    map[string]chan struct{}{},
		started: make(chan string, 16),
	}
}

func (r *imageLaneRepo) block(requestID string) chan struct{} {
	ch := make(chan struct{})
	r.mu.Lock()
	r.gate[requestID] = ch
	r.mu.Unlock()
	return ch
}

func (r *imageLaneRepo) wait(requestID string) {
	r.mu.Lock()
	ch := r.gate[requestID]
	r.mu.Unlock()
	if ch != nil {
		<-ch
	}
}

func (r *imageLaneRepo) record(cmd *UsageBillingCommand, batch bool) {
	copied := *cmd
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, &copied)
	if batch {
		return
	}
	r.applies = append(r.applies, cmd.RequestID)
}

func (r *imageLaneRepo) Apply(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.started <- cmd.RequestID
	r.wait(cmd.RequestID)
	r.record(cmd, false)
	balance := 1.0
	return &UsageBillingApplyResult{Applied: true, NewBalance: &balance}, nil
}

func (r *imageLaneRepo) ApplyBatch(_ context.Context, cmds []*UsageBillingCommand) ([]UsageBillingBatchOutcome, error) {
	ids := make([]string, len(cmds))
	for i, cmd := range cmds {
		ids[i] = cmd.RequestID
		r.started <- cmd.RequestID
		r.record(cmd, true)
	}
	r.mu.Lock()
	r.batches = append(r.batches, ids)
	r.mu.Unlock()
	out := make([]UsageBillingBatchOutcome, len(cmds))
	for i := range cmds {
		balance := 1.0
		out[i] = UsageBillingBatchOutcome{Result: &UsageBillingApplyResult{Applied: true, NewBalance: &balance}}
	}
	return out, nil
}

func (r *imageLaneRepo) ReserveBatchImageBalance(context.Context, *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	return &BatchImageBalanceHoldResult{Applied: true}, nil
}

func (r *imageLaneRepo) CaptureBatchImageBalance(context.Context, *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	return &BatchImageBalanceHoldResult{Applied: true}, nil
}

func (r *imageLaneRepo) ReleaseBatchImageBalance(context.Context, *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	return &BatchImageBalanceHoldResult{Applied: true}, nil
}

func (r *imageLaneRepo) command(requestID string) *UsageBillingCommand {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cmd := range r.commands {
		if cmd.RequestID == requestID {
			return cmd
		}
	}
	return nil
}

// A hosted text turn and a pure image-group turn for the same user settle
// through the per-user lane. Batching must not merge or repeat the charges:
// the text turn is text tokens plus exactly one gpt-image-2 fee, and the
// image group is one command priced once per delivered image.
func TestUsageBillingLane_TextImageAndImageGroupChargeOnce(t *testing.T) {
	const imagePrice = 0.2
	text := 11*2e-6 + 22*10e-6
	price2K := imagePrice
	groupID := int64(77)
	user := &User{ID: 42}
	repo := newImageLaneRepo()
	lane := NewUsageBillingLaneCombiner(repo)
	t.Cleanup(lane.Stop)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo, lane, userRepo, &openAIRecordUsageSubRepoStub{}, nil,
	)

	textKey := &APIKey{
		ID:      10,
		User:    user,
		GroupID: i64p(groupID),
		Group: &Group{
			ID:             groupID,
			Platform:       PlatformOpenAI,
			RateMultiplier: 1,
			ImagePrice2K:   &price2K,
			Description:    "对话",
		},
	}
	imageKey := &APIKey{
		ID:      11,
		User:    user,
		GroupID: i64p(groupID),
		Group: &Group{
			ID:                groupID,
			Platform:          PlatformOpenAI,
			RateMultiplier:    1,
			ImagePrice2K:      &price2K,
			ImageCountBilling: true,
			Description:       "生图",
		},
	}
	account := &Account{ID: 30, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	release := repo.block("resp_text_image")
	textErr := make(chan error, 1)
	go func() {
		textErr <- svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{
				RequestID:     "resp_text_image",
				Model:         "gpt-6-sol",
				BillingModel:  "gpt-image-2",
				UpstreamModel: "gpt-6-sol",
				Usage:         OpenAIUsage{InputTokens: 11, OutputTokens: 22},
				ImageCount:    1,
				ImageSize:     "2K",
				Duration:      time.Second,
			},
			APIKey:  textKey,
			User:    user,
			Account: account,
		})
	}()
	select {
	case got := <-repo.started:
		require.Equal(t, "resp_text_image", got)
	case <-time.After(5 * time.Second):
		t.Fatal("text image billing did not enter the lane")
	}

	imageErr := make(chan error, 1)
	followErr := make(chan error, 1)
	go func() {
		imageErr <- svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{
				RequestID:     "resp_image_group",
				Model:         "gpt-image-2",
				BillingModel:  "gpt-image-2",
				ImageEndpoint: true,
				Usage:         OpenAIUsage{InputTokens: 40, OutputTokens: 80, ImageOutputTokens: 80},
				ImageCount:    3,
				ImageSize:     "2K",
				Duration:      time.Second,
			},
			APIKey:  imageKey,
			User:    user,
			Account: account,
		})
	}()
	go func() {
		followErr <- svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{
				RequestID:     "resp_image_group_one",
				Model:         "gpt-image-2",
				BillingModel:  "gpt-image-2",
				ImageEndpoint: true,
				Usage:         OpenAIUsage{InputTokens: 40, OutputTokens: 80, ImageOutputTokens: 80},
				ImageCount:    1,
				ImageSize:     "2K",
				Duration:      time.Second,
			},
			APIKey:  imageKey,
			User:    user,
			Account: account,
		})
	}()
	waitPending(t, lane, user.ID, 2)
	close(release)

	require.NoError(t, <-textErr)
	require.NoError(t, <-imageErr)
	require.NoError(t, <-followErr)
	require.Equal(t, 0, userRepo.deductCalls, "the lane is the only balance charge")

	textCmd := repo.command("resp_text_image")
	imageCmd := repo.command("resp_image_group")
	oneCmd := repo.command("resp_image_group_one")
	require.NotNil(t, textCmd)
	require.NotNil(t, imageCmd)
	require.NotNil(t, oneCmd)
	require.Equal(t, 1, textCmd.ImageCount)
	require.Equal(t, 11, textCmd.InputTokens)
	require.Equal(t, 22, textCmd.OutputTokens)
	require.InDelta(t, text+imagePrice, textCmd.BalanceCost, 1e-9)
	require.NotEqual(t, imagePrice*2, textCmd.BalanceCost)
	require.Equal(t, 3, imageCmd.ImageCount)
	require.InDelta(t, imagePrice*3, imageCmd.BalanceCost, 1e-9)
	require.Equal(t, 1, oneCmd.ImageCount)
	require.InDelta(t, imagePrice, oneCmd.BalanceCost, 1e-9)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []string{"resp_text_image"}, repo.applies)
	require.Len(t, repo.batches, 1)
	require.ElementsMatch(t, []string{"resp_image_group", "resp_image_group_one"}, repo.batches[0])
	require.Len(t, repo.commands, 3)
}
