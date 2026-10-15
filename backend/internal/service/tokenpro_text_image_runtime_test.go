package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Optional fixture-only test against the installed Codex runtime. It uses no
// actual API key, inference service or image generation, and no user config is
// read or modified.
func TestTokenProTextImageToolsCodexRuntime(t *testing.T) {
	if os.Getenv("TOKENPRO_CODEX_BINARY") == "" {
		t.Skip("set TOKENPRO_CODEX_BINARY to run the fixture runtime probe")
	}
	probe, err := filepath.Abs(filepath.Join("testdata", "tokenpro_image_tools_runtime_probe.mjs"))
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			http.Error(w, "read fixture", http.StatusBadRequest)
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		groupID := int64(69)
		c.Set("api_key", &APIKey{KeyType: APIKeyTypeGlobal, GroupID: &groupID,
			Group: &Group{Platform: PlatformOpenAI, AllowImageGeneration: true}})
		c.Set(TokenProTextImageDeliveryContextKey, true)
		c.Set(TokenProImageTurnContextKey, &TokenProPendingImageTurn{Model: "gpt-6-luna"})
		adapted, prepareErr := prepareTokenProImageTools(c, body)
		if prepareErr != nil {
			http.Error(w, "adapt fixture", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(adapted)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", probe)
	command.Env = append(os.Environ(), "TOKENPRO_FIXTURE_ADAPTER="+server.URL, "TOKENPRO_RUNTIME_WORK="+t.TempDir())
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Log(string(output))
}
