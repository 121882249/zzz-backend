package service

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imageModelGroupRepo struct {
	GroupRepository
	group *Group
	err   error
}

func (r imageModelGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.group == nil || r.group.ID != id {
		return nil, ErrGroupNotFound
	}
	copied := *r.group
	return &copied, nil
}

type imageModelAccountRepo struct {
	AccountRepository
	accounts []Account
	err      error
}

func (r imageModelAccountRepo) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]Account(nil), r.accounts...), nil
}

func imageModelAccount(mapping map[string]any) Account {
	return Account{
		ID: 40, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": mapping},
	}
}

func TestTokenProImageGroupContainsDescription(t *testing.T) {
	contained := &Group{Platform: PlatformOpenAI, Description: "高速 生图", Status: StatusActive}
	require.True(t, TokenProImageGroup(contained))
	require.False(t, TokenProPureImageGroup(contained))
	exact := &Group{Platform: PlatformOpenAI, Description: "生图", Status: StatusActive}
	require.True(t, TokenProImageGroup(exact))
	require.True(t, TokenProPureImageGroup(exact))
	require.False(t, TokenProImageGroup(&Group{Platform: PlatformOpenAI, Description: "图片"}))
	require.False(t, TokenProImageGroup(&Group{Platform: PlatformAnthropic, Description: "生图"}))
}

func TestCanonicalTokenProImageModel(t *testing.T) {
	group := &Group{ID: 65, Platform: PlatformOpenAI, Description: "生图", Status: StatusActive}
	mapping := map[string]any{
		"gpt-image-1":            "gpt-image-1",
		"gpt-image-1.5":          "gpt-image-1.5",
		"gpt-image-2":            "gpt-image-2",
		"gpt-image-2.5-flare":    "gpt-image-2.5-flare",
		"gpt-image-2.5-sunburst": "gpt-image-2.5-sunburst",
	}
	svc := &GatewayService{
		groupRepo:   imageModelGroupRepo{group: group},
		accountRepo: imageModelAccountRepo{accounts: []Account{imageModelAccount(mapping)}},
	}
	got, err := svc.CanonicalTokenProImageModel(context.Background(), 65, "GPT-Image-2.5-Flare")
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2.5-flare", got)

	contained := *group
	contained.Description = "高速 生图"
	containedSvc := &GatewayService{
		groupRepo:   imageModelGroupRepo{group: &contained},
		accountRepo: imageModelAccountRepo{accounts: []Account{imageModelAccount(mapping)}},
	}
	got, err = containedSvc.CanonicalTokenProImageModel(context.Background(), 65, "gpt-image-2.5-sunburst")
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2.5-sunburst", got)

	cases := []struct {
		name    string
		group   *Group
		groupID int64
		model   string
		repoErr error
		want    string
	}{
		{name: "text model", group: group, groupID: 65, model: "gpt-5.4", want: "指定的模型不是生图模型"},
		{name: "unmapped image model", group: group, groupID: 65, model: "gpt-image-9", want: "该生图分组不支持这个图片模型"},
		{name: "not an image group", group: &Group{ID: 16, Platform: PlatformOpenAI, Description: "对话", Status: StatusActive}, groupID: 16, model: "gpt-image-2", want: "当前分组不是生图分组"},
		{name: "inactive", group: &Group{ID: 65, Platform: PlatformOpenAI, Description: "生图", Status: "disabled"}, groupID: 65, model: "gpt-image-2", want: "生图分组不存在或已停用"},
		{name: "missing group", groupID: 0, model: "gpt-image-2", want: "生图请求缺少有效分组"},
		{name: "unknown group", groupID: 99, model: "gpt-image-2", want: "生图分组不存在或已停用"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			local := &GatewayService{
				groupRepo:   imageModelGroupRepo{group: tc.group},
				accountRepo: imageModelAccountRepo{accounts: []Account{imageModelAccount(mapping)}},
			}
			_, err := local.CanonicalTokenProImageModel(context.Background(), tc.groupID, tc.model)
			var modelErr *TokenProImageModelError
			require.ErrorAs(t, err, &modelErr)
			require.Contains(t, modelErr.Message, tc.want)
			require.Contains(t, modelErr.Message, "没有改用其他模型")
		})
	}

	allowlisted := *group
	allowlisted.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"gpt-image-1"}}
	denied := &GatewayService{
		groupRepo:   imageModelGroupRepo{group: &allowlisted},
		accountRepo: imageModelAccountRepo{accounts: []Account{imageModelAccount(mapping)}},
	}
	_, err = denied.CanonicalTokenProImageModel(context.Background(), 65, "gpt-image-2.5-flare")
	var modelErr *TokenProImageModelError
	require.ErrorAs(t, err, &modelErr)
	require.Contains(t, modelErr.Message, "该生图分组不支持这个图片模型")

	_, err = (&GatewayService{
		groupRepo:   imageModelGroupRepo{err: errors.New("db down")},
		accountRepo: imageModelAccountRepo{accounts: []Account{imageModelAccount(mapping)}},
	}).CanonicalTokenProImageModel(context.Background(), 65, "gpt-image-2")
	require.ErrorIs(t, err, ErrTokenProImageModelInternal)
	require.NotErrorIs(t, err, ErrGroupNotFound)
}

func TestRewriteOpenAIImagesModelJSONAndMultipart(t *testing.T) {
	rewritten, contentType, err := RewriteOpenAIImagesModel([]byte(`{"model":"gpt-image-2","prompt":"cat"}`), "application/json", "gpt-image-2.5-flare")
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "gpt-image-2.5-flare", gjson.GetBytes(rewritten, "model").String())
	require.Equal(t, "cat", gjson.GetBytes(rewritten, "prompt").String())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "edit the cat"))
	require.NoError(t, writer.Close())
	rewritten, contentType, err = RewriteOpenAIImagesModel(body.Bytes(), writer.FormDataContentType(), "gpt-image-2.5-sunburst")
	require.NoError(t, err)
	_, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(rewritten), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2.5-sunburst", form.Value["model"][0])
	require.Equal(t, "edit the cat", form.Value["prompt"][0])
}
