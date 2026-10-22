//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculateOpenAIRecordUsageCost_TextModelImageAddsPerImagePrice(t *testing.T) {
	t.Parallel()

	svc := &OpenAIGatewayService{billingService: newTestBillingService()}
	apiKey := &APIKey{Group: &Group{}}
	// gpt-6-sol fallback: input 2e-6, output 10e-6.
	// 11 in + 22 out = 0.000242. Omitted size bills as 2K: 0.134 * 1.5 = 0.201.
	text := 11*2e-6 + 22*10e-6
	image := defaultImageGenerationPrice * 1.5
	cost, err := svc.calculateOpenAIRecordUsageCost(
		context.Background(),
		&OpenAIForwardResult{
			Model:        "gpt-6-sol",
			BillingModel: "gpt-image-2",
			ImageCount:   1,
			ImageSize:    "2K",
		},
		apiKey,
		[]string{"gpt-image-2", "gpt-6-sol"},
		1,
		1,
		1,
		1,
		UsageTokens{InputTokens: 11, OutputTokens: 22},
		"",
		boolPtr(false),
		false,
		time.Time{},
	)
	require.NoError(t, err)
	require.NotNil(t, cost)
	require.InDelta(t, 11*2e-6, cost.InputCost, 1e-12)
	require.InDelta(t, 22*10e-6, cost.OutputCost, 1e-12)
	require.InDelta(t, text+image, cost.TotalCost, 1e-12)
	require.InDelta(t, text+image, cost.ActualCost, 1e-12)
	require.Equal(t, string(BillingModeImage), cost.BillingMode)
}

func TestCalculateOpenAIRecordUsageCost_TextModelImageUsesSeparateMultipliers(t *testing.T) {
	t.Parallel()

	svc := &OpenAIGatewayService{billingService: newTestBillingService()}
	text := 11*2e-6 + 22*10e-6
	image := defaultImageGenerationPrice * 1.5
	cost, err := svc.calculateOpenAIRecordUsageCost(
		context.Background(),
		&OpenAIForwardResult{Model: "gpt-6-sol", BillingModel: "gpt-image-2", ImageCount: 1, ImageSize: "2K"},
		&APIKey{Group: &Group{}},
		[]string{"gpt-image-2", "gpt-6-sol"},
		2, // token multiplier
		3, // image multiplier
		1,
		1,
		UsageTokens{InputTokens: 11, OutputTokens: 22},
		"",
		boolPtr(false),
		false,
		time.Time{},
	)
	require.NoError(t, err)
	require.InDelta(t, text+image, cost.TotalCost, 1e-12)
	require.InDelta(t, text*2+image*3, cost.ActualCost, 1e-12)
}

func TestCalculateOpenAIRecordUsageCost_PureImageModelDoesNotAddTokenCost(t *testing.T) {
	t.Parallel()

	svc := &OpenAIGatewayService{billingService: newTestBillingService()}
	image := defaultImageGenerationPrice * 1.5
	cost, err := svc.calculateOpenAIRecordUsageCost(
		context.Background(),
		&OpenAIForwardResult{Model: "gpt-image-2", BillingModel: "gpt-image-2", ImageCount: 1, ImageSize: "2K"},
		&APIKey{Group: &Group{}},
		[]string{"gpt-image-2"},
		1,
		1,
		1,
		1,
		UsageTokens{InputTokens: 11, OutputTokens: 22},
		"",
		boolPtr(false),
		false,
		time.Time{},
	)
	require.NoError(t, err)
	require.InDelta(t, image, cost.TotalCost, 1e-12)
	require.Zero(t, cost.InputCost)
	require.Zero(t, cost.OutputCost)
}

func TestOpenAIRecordUsage_TextModelImageIsOneRow(t *testing.T) {
	imagePrice := 0.5
	groupID := int64(77)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	text := 11*2e-6 + 22*10e-6

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:     "resp_text_plus_image",
			Model:         "gpt-6-sol",
			BillingModel:  "gpt-image-2",
			UpstreamModel: "gpt-6-sol",
			Usage:         OpenAIUsage{InputTokens: 11, OutputTokens: 22},
			ImageCount:    1,
			ImageSize:     "2K",
			Duration:      time.Second,
		},
		APIKey: &APIKey{
			ID:      10,
			GroupID: i64p(groupID),
			Group: &Group{
				ID:             groupID,
				RateMultiplier: 1,
				ImagePrice2K:   &imagePrice,
			},
		},
		User:    &User{ID: 20},
		Account: &Account{ID: 30, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	})
	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, userRepo.deductCalls)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "gpt-6-sol", usageRepo.lastLog.Model)
	require.Equal(t, 11, usageRepo.lastLog.InputTokens)
	require.Equal(t, 22, usageRepo.lastLog.OutputTokens)
	require.Equal(t, 1, usageRepo.lastLog.ImageCount)
	require.NotNil(t, usageRepo.lastLog.ImageSize)
	require.Equal(t, "2K", *usageRepo.lastLog.ImageSize)
	require.InDelta(t, 11*2e-6, usageRepo.lastLog.InputCost, 1e-12)
	require.InDelta(t, 22*10e-6, usageRepo.lastLog.OutputCost, 1e-12)
	require.InDelta(t, text+imagePrice, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, text+imagePrice, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, text+imagePrice, userRepo.lastAmount, 1e-12)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeImage), *usageRepo.lastLog.BillingMode)
}
