//go:build unit

package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type forceReceiptYieldWriter struct{ gin.ResponseWriter }

func (w forceReceiptYieldWriter) Write(data []byte) (int, error) {
	// Force the real client to yield so exec -> wait continuation is exercised
	// without waiting two minutes per test image. Production uses 120000 ms.
	changed := bytes.ReplaceAll(data, []byte(`yield_time_ms\": 120000`), []byte(`yield_time_ms\": 1`))
	_, err := w.ResponseWriter.Write(changed)
	return len(data), err
}

func TestTokenProReceiptRealCodex(t *testing.T) {
	codex := os.Getenv("TOKENPRO_RECEIPT_CODEX")
	if codex == "" {
		t.Skip("set TOKENPRO_RECEIPT_CODEX to run the installed-client integration")
	}
	gin.SetMode(gin.TestMode)
	rdb := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: rdb.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := repository.NewTokenProImageTurnStore(client)
	var mu sync.Mutex
	var requestSizes []int
	var requestImageInputs []int
	var hashes []string
	var generationCalls, editCalls, waitCalls, receiptDispatches int
	key := &service.APIKey{ID: 1, UserID: 1, Key: "fixture", KeyType: service.APIKeyTypeGlobal}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyAPIKey), key); c.Next() })
	r.Use(middleware2.TokenProDirectRoute(store))
	bind := func(c *gin.Context) *service.APIKey {
		group, _ := strconv.ParseInt(c.GetHeader("X-TokenPro-Group-Id"), 10, 64)
		if group != 65 && group != 66 {
			c.AbortWithStatus(403)
			return nil
		}
		resolved := *key
		resolved.GroupID = &group
		resolved.Group = &service.Group{ID: group, Platform: service.PlatformOpenAI, Description: "生图", AllowImageGeneration: true}
		if err := service.BindTokenProImageTurn(c, &resolved); err != nil {
			c.AbortWithStatus(409)
			return nil
		}
		return &resolved
	}
	r.POST("/v1/responses", func(c *gin.Context) {
		resolved := bind(c)
		if resolved == nil {
			return
		}
		body, _ := io.ReadAll(c.Request.Body)
		mu.Lock()
		if len(requestSizes) == 0 {
			for _, tool := range gjson.GetBytes(body, "tools").Array() {
				t.Logf("client tool: type=%s name=%s namespace=%s children=%s", tool.Get("type").String(), tool.Get("name").String(), tool.Get("namespace").String(), tool.Get("tools.#.name").Raw)
			}
		}
		requestSizes = append(requestSizes, len(body))
		requestImageInputs = append(requestImageInputs, bytes.Count(body, []byte(`"type":"input_image"`)))
		if bytes.Contains(body, []byte(`call_tp_wait_`)) {
			waitCalls++
		}
		mu.Unlock()
		c.Writer = forceReceiptYieldWriter{c.Writer}
		started := false
		h := &OpenAIGatewayHandler{}
		require.True(t, h.dispatchTokenProNativeImage(c, resolved, gjson.GetBytes(body, "model").String(), service.PlatformOpenAI, body, true, &started))
		if c.Writer.Header().Get("X-TokenPro-Image-Receipt") == "text-v1" {
			mu.Lock()
			receiptDispatches++
			mu.Unlock()
		}
	})
	serveImage := func(c *gin.Context, endpoint string) {
		resolved := bind(c)
		if resolved == nil {
			return
		}
		prompt := ""
		multipartRequest := strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data")
		if endpoint == "/v1/images/edits" && multipartRequest {
			if formErr := c.Request.ParseMultipartForm(20 << 20); formErr != nil {
				c.JSON(400, gin.H{"error": gin.H{"message": "invalid edit multipart"}})
				return
			}
			prompt = c.Request.FormValue("prompt")
			images := 0
			fields := make([]string, 0, len(c.Request.MultipartForm.File))
			for field, files := range c.Request.MultipartForm.File {
				fields = append(fields, field)
				if field != "image" && !strings.HasPrefix(field, "image[") {
					continue
				}
				images += len(files)
			}
			if images == 0 {
				c.JSON(400, gin.H{"error": gin.H{"message": "missing edit image; fields=" + strings.Join(fields, ",")}})
				return
			}
		} else {
			body, _ := io.ReadAll(c.Request.Body)
			prompt = gjson.GetBytes(body, "prompt").String()
			if endpoint == "/v1/images/edits" && len(gjson.GetBytes(body, "images").Array()) == 0 {
				c.JSON(400, gin.H{"error": gin.H{"message": "missing JSON edit image"}})
				return
			}
		}
		finish, err := service.ClaimTokenProImageReceipt(c, resolved, &service.OpenAIImagesRequest{Endpoint: endpoint, Prompt: prompt, Multipart: multipartRequest})
		if err != nil || finish == nil {
			c.JSON(409, gin.H{"error": gin.H{"message": "receipt not prepared"}})
			return
		}
		defer func() { require.NoError(t, finish(false)) }()
		mu.Lock()
		if endpoint == "/v1/images/edits" {
			editCalls++
		} else {
			generationCalls++
		}
		seed := uint32(generationCalls + editCalls)
		mu.Unlock()
		time.Sleep(1200 * time.Millisecond)
		if strings.TrimSpace(prompt) == "FAIL" {
			c.JSON(400, gin.H{"error": gin.H{"message": "fixture image generation failed"}})
			return
		}
		bitmap := image.NewNRGBA(image.Rect(0, 0, 1024, 768))
		for y := 0; y < 768; y++ {
			for x := 0; x < 1024; x++ {
				seed ^= seed << 13
				seed ^= seed >> 17
				seed ^= seed << 5
				bitmap.SetNRGBA(x, y, color.NRGBA{R: byte(seed), G: byte(seed >> 8), B: byte(seed >> 16), A: 255})
			}
		}
		var output bytes.Buffer
		require.NoError(t, png.Encode(&output, bitmap))
		mu.Lock()
		hashes = append(hashes, service.TokenProImageReceiptHash(output.Bytes()))
		mu.Unlock()
		c.JSON(200, gin.H{"created": 1, "data": []any{gin.H{"b64_json": base64.StdEncoding.EncodeToString(output.Bytes())}}})
		c.Writer.Flush()
		// Exercise the receipt race: the client may receive its image before
		// the handler records completion, as on the real ForwardImages path.
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, finish(true))
	}
	r.POST("/v1/images/generations", func(c *gin.Context) { serveImage(c, "/v1/images/generations") })
	r.POST("/v1/images/edits", func(c *gin.Context) { serveImage(c, "/v1/images/edits") })
	r.GET("/probe", func(c *gin.Context) {
		mu.Lock()
		defer mu.Unlock()
		c.JSON(200, gin.H{"sizes": requestSizes, "image_inputs": requestImageInputs, "hashes": hashes, "generation_calls": generationCalls, "edit_calls": editCalls,
			"wait_calls": waitCalls, "receipt_dispatches": receiptDispatches})
	})
	server := httptest.NewServer(r)
	defer server.Close()
	python, err := exec.LookPath("python3")
	require.NoError(t, err)
	catalog, err := filepath.Abs("testdata/native-receipt-catalog.json")
	require.NoError(t, err)
	command := exec.Command(python, "testdata/native_receipt_probe.py", codex, server.URL, catalog)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 6, generationCalls)
	require.Equal(t, 1, editCalls, "only an explicitly attached canvas image may enter the edit path")
	require.Len(t, hashes, 6)
	require.Greater(t, receiptDispatches, 6)
	require.Greater(t, waitCalls, 0, "long-running executions must use wait, not claim failure")
	for index, size := range requestSizes {
		if requestImageInputs[index] == 0 {
			require.Less(t, size, 100000, "generated image bytes must not be uploaded during text-only receipt continuation")
		} else {
			require.Equal(t, 1, requestImageInputs[index], "an edit may carry its one source image, never the generated result again")
		}
	}
	if path := os.Getenv("TOKENPRO_RECEIPT_REPORT"); path != "" {
		data, _ := json.MarshalIndent(gin.H{"sizes": requestSizes, "generation_calls": generationCalls,
			"edit_calls": editCalls, "saved_image_count": len(hashes), "wait_calls": waitCalls, "receipt_dispatches": receiptDispatches}, "", "  ")
		require.NoError(t, os.WriteFile(path, data, 0600))
	}
}
