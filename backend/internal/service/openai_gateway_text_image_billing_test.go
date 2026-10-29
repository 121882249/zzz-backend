//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
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

// Production shape: group rate 0.3, independent image rate 1, group 2K price
// 0.08, and a token-priced gpt-6-sol channel (image_output_price 3e-5).
// billing_model_source must not hide gpt-image-2. 515 image tokens are not
// subtracted from 162 text output tokens and are not priced at 3e-5.
func TestOpenAIRecordUsage_HostedTextImageIgnoresBillingModelSource(t *testing.T) {
	const (
		inputTokens       = 1000
		outputTokens      = 162
		imageOutputTokens = 515
		groupRate         = 0.3
		imageRate         = 1.0
		imagePrice2K      = 0.08
	)
	inputPrice := 3e-6
	outputPrice := 1.5e-5
	cacheReadPrice := 3e-7
	imageOutputPrice := 3e-5
	decoyInput := 9e-6
	decoyOutput := 9e-5
	textList := float64(inputTokens)*inputPrice + float64(outputTokens)*outputPrice
	textActual := textList * groupRate
	imageActual := imagePrice2K * imageRate

	groupID := int64(16016)
	price1K := 0.03
	price2K := imagePrice2K
	price4K := 0.10
	group := &Group{
		ID:                   groupID,
		Platform:             PlatformOpenAI,
		RateMultiplier:       groupRate,
		ImageRateIndependent: true,
		ImageRateMultiplier:  imageRate,
		ImagePrice1K:         &price1K,
		ImagePrice2K:         &price2K,
		ImagePrice4K:         &price4K,
	}

	tests := []struct {
		name       string
		source     string
		original   string
		mapped     string
		pricedName string
	}{
		{
			name:       "requested",
			source:     BillingModelSourceRequested,
			original:   "gpt-6-sol",
			mapped:     "gpt-6-sol-mapped",
			pricedName: "gpt-6-sol",
		},
		{
			name:       "channel_mapped",
			source:     BillingModelSourceChannelMapped,
			original:   "gpt-6-sol-alias",
			mapped:     "gpt-6-sol",
			pricedName: "gpt-6-sol",
		},
		{
			name:       "upstream",
			source:     BillingModelSourceUpstream,
			original:   "gpt-6-sol",
			mapped:     "",
			pricedName: "gpt-6-sol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = newHostedTextImageChannelResolver(t, groupID, map[string]hostedTextImageChannelPrices{
				"gpt-6-sol":        {input: inputPrice, output: outputPrice, cacheRead: cacheReadPrice, imageOutput: imageOutputPrice},
				"gpt-6-sol-mapped": {input: decoyInput, output: decoyOutput, cacheRead: cacheReadPrice, imageOutput: imageOutputPrice},
				"gpt-6-sol-alias":  {input: decoyInput, output: decoyOutput, cacheRead: cacheReadPrice, imageOutput: imageOutputPrice},
			})

			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID:     "resp_hosted_" + tt.name,
					Model:         tt.original,
					BillingModel:  "gpt-image-2",
					UpstreamModel: "gpt-6-sol",
					Usage: OpenAIUsage{
						InputTokens:       inputTokens,
						OutputTokens:      outputTokens,
						ImageOutputTokens: imageOutputTokens,
					},
					ImageCount: 1,
					ImageSize:  "2K",
					Duration:   time.Second,
				},
				APIKey: &APIKey{
					ID:      10,
					GroupID: i64p(groupID),
					Group:   group,
				},
				User:    &User{ID: 20},
				Account: &Account{ID: 30, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				ChannelUsageFields: ChannelUsageFields{
					OriginalModel:      tt.original,
					ChannelMappedModel: tt.mapped,
					BillingModelSource: tt.source,
				},
			})
			require.NoError(t, err)
			require.Equal(t, 1, usageRepo.calls)
			require.Equal(t, 1, userRepo.deductCalls)
			log := usageRepo.lastLog
			require.NotNil(t, log)
			require.NotNil(t, log.BillingMode)
			require.Equal(t, string(BillingModeImage), *log.BillingMode)
			require.Equal(t, 1, log.ImageCount)
			require.NotNil(t, log.ImageSize)
			require.Equal(t, "2K", *log.ImageSize)
			require.Equal(t, imageOutputTokens, log.ImageOutputTokens)
			require.Equal(t, outputTokens, log.OutputTokens)
			require.InDelta(t, float64(inputTokens)*inputPrice, log.InputCost, 1e-12)
			require.InDelta(t, float64(outputTokens)*outputPrice, log.OutputCost, 1e-12, "text output tokens must not be reduced by image_output_tokens")
			require.Zero(t, log.ImageOutputCost, "image_output_price must not bill the hosted image")
			require.InDelta(t, textList+imagePrice2K, log.TotalCost, 1e-12)
			require.InDelta(t, textActual+imageActual, log.ActualCost, 1e-12)
			require.InDelta(t, imageRate, log.RateMultiplier, 1e-12)
			require.InDelta(t, textActual+imageActual, userRepo.lastAmount, 1e-12)
			require.NotEqual(t, tt.pricedName, "")
		})
	}
}

func TestOpenAIRecordUsage_PureImageTokenChannelStillSkipsPerImageFee(t *testing.T) {
	groupID := int64(16017)
	imagePrice := 0.08
	inputPrice := 3e-6
	outputPrice := 1.5e-5
	imageOutputPrice := 3e-5
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = newHostedTextImageChannelResolver(t, groupID, map[string]hostedTextImageChannelPrices{
		"gpt-image-2": {input: inputPrice, output: outputPrice, imageOutput: imageOutputPrice},
	})

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:    "resp_pure_image_token_channel",
			Model:        "gpt-image-2",
			BillingModel: "gpt-image-2",
			Usage: OpenAIUsage{
				InputTokens:       10,
				OutputTokens:      20,
				ImageOutputTokens: 20,
			},
			ImageCount: 1,
			ImageSize:  "2K",
			Duration:   time.Second,
		},
		APIKey: &APIKey{
			ID:      11,
			GroupID: i64p(groupID),
			Group: &Group{
				ID:                   groupID,
				RateMultiplier:       0.3,
				ImageRateIndependent: true,
				ImageRateMultiplier:  1,
				ImagePrice2K:         &imagePrice,
			},
		},
		User:    &User{ID: 21},
		Account: &Account{ID: 31, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "gpt-image-2",
			BillingModelSource: BillingModelSourceRequested,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeToken), *usageRepo.lastLog.BillingMode)
	require.InDelta(t, 10*inputPrice, usageRepo.lastLog.InputCost, 1e-12)
	require.Zero(t, usageRepo.lastLog.OutputCost)
	require.InDelta(t, 20*imageOutputPrice, usageRepo.lastLog.ImageOutputCost, 1e-12)
	require.NotEqual(t, imagePrice, usageRepo.lastLog.TotalCost)
}

// image_count_billing must not replace a hosted text+image row with an
// image-only charge, and must not add a second image fee. The text cost uses
// the token rate once; the gpt-image-2 fee uses the image rate once.
func TestOpenAIRecordUsage_HostedBridgeIgnoresImageCountBilling(t *testing.T) {
	const (
		inputTokens       = 1000
		outputTokens      = 162
		imageOutputTokens = 515
		groupRate         = 0.3
		imageRate         = 1.0
		imagePrice2K      = 0.08
		inputPrice        = 3e-6
		outputPrice       = 1.5e-5
		imageOutputPrice  = 3e-5
	)
	textList := float64(inputTokens)*inputPrice + float64(outputTokens)*outputPrice
	textActual := textList * groupRate
	imageActual := imagePrice2K * imageRate
	groupID := int64(16019)
	price2K := imagePrice2K

	cases := []struct {
		name     string
		endpoint bool
		switchOn bool
	}{
		{name: "switch off", endpoint: false, switchOn: false},
		{name: "switch on mixed turn", endpoint: false, switchOn: true},
		{name: "switch on does not bypass bridge", endpoint: true, switchOn: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = newHostedTextImageChannelResolver(t, groupID, map[string]hostedTextImageChannelPrices{
				"gpt-6-sol": {input: inputPrice, output: outputPrice, imageOutput: imageOutputPrice},
			})
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID:     "resp_bridge_" + tc.name,
					Model:         "gpt-6-sol",
					BillingModel:  "gpt-image-2",
					UpstreamModel: "gpt-6-sol",
					ImageEndpoint: tc.endpoint,
					Usage: OpenAIUsage{
						InputTokens:       inputTokens,
						OutputTokens:      outputTokens,
						ImageOutputTokens: imageOutputTokens,
					},
					ImageCount: 1,
					ImageSize:  "2K",
					Duration:   time.Second,
				},
				APIKey: &APIKey{
					ID:      13,
					GroupID: i64p(groupID),
					Group: &Group{
						ID:                   groupID,
						Platform:             PlatformOpenAI,
						RateMultiplier:       groupRate,
						ImageRateIndependent: true,
						ImageRateMultiplier:  imageRate,
						ImagePrice2K:         &price2K,
						ImageCountBilling:    tc.switchOn,
					},
				},
				User:    &User{ID: 24},
				Account: &Account{ID: 34, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				ChannelUsageFields: ChannelUsageFields{
					OriginalModel:      "gpt-6-sol",
					BillingModelSource: BillingModelSourceRequested,
				},
			})
			require.NoError(t, err)
			require.Equal(t, 1, usageRepo.calls)
			require.Equal(t, 1, userRepo.deductCalls)
			log := usageRepo.lastLog
			require.NotNil(t, log)
			require.NotNil(t, log.BillingMode)
			require.Equal(t, string(BillingModeImage), *log.BillingMode)
			require.Equal(t, 1, log.ImageCount)
			require.Equal(t, outputTokens, log.OutputTokens)
			require.InDelta(t, float64(inputTokens)*inputPrice, log.InputCost, 1e-12)
			require.InDelta(t, float64(outputTokens)*outputPrice, log.OutputCost, 1e-12)
			require.Zero(t, log.ImageOutputCost)
			require.InDelta(t, textList+imagePrice2K, log.TotalCost, 1e-12)
			require.InDelta(t, textActual+imageActual, log.ActualCost, 1e-12)
			require.InDelta(t, textActual+imageActual, userRepo.lastAmount, 1e-12)
			require.NotEqual(t, imagePrice2K*2, log.TotalCost)
		})
	}
}

// A dedicated image endpoint with the switch on stays on one per-image fee.
// A token-priced channel card must not be added on top of that fee.
func TestOpenAIRecordUsage_ImageCountBillingDoesNotStackOnPureImageEndpoint(t *testing.T) {
	groupID := int64(16020)
	imagePrice := 0.08
	inputPrice := 3e-6
	outputPrice := 1.5e-5
	imageOutputPrice := 3e-5
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = newHostedTextImageChannelResolver(t, groupID, map[string]hostedTextImageChannelPrices{
		"gpt-image-2": {input: inputPrice, output: outputPrice, imageOutput: imageOutputPrice},
	})
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:     "resp_pure_count_switch",
			Model:         "gpt-image-2",
			BillingModel:  "gpt-image-2",
			ImageEndpoint: true,
			Usage: OpenAIUsage{
				InputTokens:       10,
				OutputTokens:      20,
				ImageOutputTokens: 20,
			},
			ImageCount: 1,
			ImageSize:  "2K",
			Duration:   time.Second,
		},
		APIKey: &APIKey{
			ID:      14,
			GroupID: i64p(groupID),
			Group: &Group{
				ID:                   groupID,
				Platform:             PlatformOpenAI,
				RateMultiplier:       0.3,
				ImageRateIndependent: true,
				ImageRateMultiplier:  1,
				ImagePrice2K:         &imagePrice,
				ImageCountBilling:    true,
			},
		},
		User:    &User{ID: 25},
		Account: &Account{ID: 35, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "gpt-image-2",
			BillingModelSource: BillingModelSourceRequested,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, userRepo.deductCalls)
	log := usageRepo.lastLog
	require.NotNil(t, log)
	require.NotNil(t, log.BillingMode)
	require.Equal(t, string(BillingModeImage), *log.BillingMode)
	require.Zero(t, log.InputCost)
	require.Zero(t, log.OutputCost)
	require.Zero(t, log.ImageOutputCost)
	require.InDelta(t, imagePrice, log.TotalCost, 1e-12)
	require.InDelta(t, imagePrice, userRepo.lastAmount, 1e-12)
}

// Gemini and other non-GPT image models stay on the per-image price even when
// the turn also reports text tokens and billing_model_source is requested.
// A token-mode channel card is the only path that prices those tokens instead.
// The image multiplier is applied once and is not combined with the text rate.
func TestOpenAIRecordUsage_GeminiImageKeepsPerImageUnlessTokenChannel(t *testing.T) {
	const (
		inputTokens       = 1000
		outputTokens      = 200
		imageOutputTokens = 50
		groupRate         = 0.3
		imageRate         = 1.5
		imagePrice2K      = 0.08
		inputPrice        = 3e-6
		outputPrice       = 1.5e-5
		imageOutputPrice  = 3e-5
	)
	groupID := int64(16018)
	price2K := imagePrice2K
	group := &Group{
		ID:                   groupID,
		Platform:             PlatformGemini,
		RateMultiplier:       groupRate,
		ImageRateIndependent: true,
		ImageRateMultiplier:  imageRate,
		ImagePrice2K:         &price2K,
	}
	result := &OpenAIForwardResult{
		RequestID:    "resp_gemini_image",
		Model:        "gemini-3-pro-image",
		BillingModel: "gemini-3-pro-image",
		Usage: OpenAIUsage{
			InputTokens:       inputTokens,
			OutputTokens:      outputTokens,
			ImageOutputTokens: imageOutputTokens,
		},
		ImageCount: 1,
		ImageSize:  "2K",
		Duration:   time.Second,
	}
	apiKey := &APIKey{
		ID:      12,
		GroupID: i64p(groupID),
		Group:   group,
	}
	fields := ChannelUsageFields{
		OriginalModel:      "gemini-3-pro-image",
		BillingModelSource: BillingModelSourceRequested,
	}

	t.Run("per-image", func(t *testing.T) {
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		userRepo := &openAIRecordUsageUserRepoStub{}
		svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result:             result,
			APIKey:             apiKey,
			User:               &User{ID: 22},
			Account:            &Account{ID: 32, Platform: PlatformGemini, Type: AccountTypeAPIKey},
			ChannelUsageFields: fields,
		})
		require.NoError(t, err)
		require.Equal(t, 1, usageRepo.calls)
		require.Equal(t, 1, userRepo.deductCalls)
		log := usageRepo.lastLog
		require.NotNil(t, log)
		require.NotNil(t, log.BillingMode)
		require.Equal(t, string(BillingModeImage), *log.BillingMode)
		require.Equal(t, 1, log.ImageCount)
		require.Zero(t, log.InputCost)
		require.Zero(t, log.OutputCost)
		require.Zero(t, log.ImageOutputCost)
		require.InDelta(t, imagePrice2K, log.TotalCost, 1e-12)
		require.InDelta(t, imagePrice2K*imageRate, log.ActualCost, 1e-12)
		require.InDelta(t, imageRate, log.RateMultiplier, 1e-12)
		require.InDelta(t, imagePrice2K*imageRate, userRepo.lastAmount, 1e-12)
	})

	t.Run("token channel", func(t *testing.T) {
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		userRepo := &openAIRecordUsageUserRepoStub{}
		svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
		svc.resolver = newHostedTextImageChannelResolver(t, groupID, map[string]hostedTextImageChannelPrices{
			"gemini-3-pro-image": {input: inputPrice, output: outputPrice, imageOutput: imageOutputPrice},
		})
		textOutput := outputTokens - imageOutputTokens
		list := float64(inputTokens)*inputPrice + float64(textOutput)*outputPrice + float64(imageOutputTokens)*imageOutputPrice
		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result:             result,
			APIKey:             apiKey,
			User:               &User{ID: 23},
			Account:            &Account{ID: 33, Platform: PlatformGemini, Type: AccountTypeAPIKey},
			ChannelUsageFields: fields,
		})
		require.NoError(t, err)
		require.Equal(t, 1, usageRepo.calls)
		require.Equal(t, 1, userRepo.deductCalls)
		log := usageRepo.lastLog
		require.NotNil(t, log)
		require.NotNil(t, log.BillingMode)
		require.Equal(t, string(BillingModeToken), *log.BillingMode)
		require.InDelta(t, float64(inputTokens)*inputPrice, log.InputCost, 1e-12)
		require.InDelta(t, float64(textOutput)*outputPrice, log.OutputCost, 1e-12)
		require.InDelta(t, float64(imageOutputTokens)*imageOutputPrice, log.ImageOutputCost, 1e-12)
		require.InDelta(t, list, log.TotalCost, 1e-12)
		require.InDelta(t, list*groupRate, log.ActualCost, 1e-12)
		require.NotEqual(t, imagePrice2K, log.TotalCost)
		require.InDelta(t, groupRate, log.RateMultiplier, 1e-12)
		require.InDelta(t, list*groupRate, userRepo.lastAmount, 1e-12)
	})
}

type hostedTextImageChannelPrices struct {
	input       float64
	output      float64
	cacheRead   float64
	imageOutput float64
}

func newHostedTextImageChannelResolver(t *testing.T, groupID int64, models map[string]hostedTextImageChannelPrices) *ModelPricingResolver {
	t.Helper()
	cache := newEmptyChannelCache()
	for model, price := range models {
		inputPrice := price.input
		outputPrice := price.output
		cacheReadPrice := price.cacheRead
		imageOutputPrice := price.imageOutput
		entry := &ChannelModelPricing{
			BillingMode:      BillingModeToken,
			InputPrice:       &inputPrice,
			OutputPrice:      &outputPrice,
			ImageOutputPrice: &imageOutputPrice,
		}
		if price.cacheRead > 0 {
			entry.CacheReadPrice = &cacheReadPrice
		}
		cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}] = entry
	}
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = ""
	cache.loadedAt = time.Now()
	cs := &ChannelService{}
	cs.cache.Store(cache)
	return NewModelPricingResolver(cs, NewBillingService(&config.Config{}, nil))
}
