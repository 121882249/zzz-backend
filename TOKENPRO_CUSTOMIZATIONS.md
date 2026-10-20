# TokenPro 二次开发清单

当前分支以 Sub2API `v0.2.17`（`060d016`）为底层版本，发行名 `TokenPro-R83-v0.2.17`。`backend/internal/service/update_service.go` 中的 `tokenProUpstreamVersion` 为 `0.2.17`，`backend/cmd/server/VERSION` 为 `TokenPro-R83-v0.2.17`。此文件用于后续升级时区分上游改动和本项目改动。

下文路径中 `backend/internal/` 简写为 `be/`，`frontend/src/` 简写为 `fe/`。

## 1. TokenPro 全局 API Key

- API Key 使用独立 `key_type` 区分 `global` 与普通 `group` 类型，不依赖名称判断。
- 系统全局 `TokenPro` Key 不可删除；用户可通过重置功能轮换 Key 值，重置保留原记录 ID 并强制 `group_id` 为空。
- 全局 Key 的 `group_id` 为空；普通 Key 继续固定绑定原分组。
- 注册及存量用户补齐全局 Key 的服务逻辑和幂等数据库迁移。
- 每个请求根据真实模型独立解析可用分组和账号，不把结果回写到 Key。
- 全局 Key 与普通分组 Key 一样执行用户设置中的并发限制；`concurrency > 0` 时按用户统一占槽，`0` 表示不限制，账号侧并发仍独立生效。
- 没有可用分组时返回“当前模型没有可用分组或有效订阅。”。
- API Key 鉴权缓存保留 Key 类型、模型白名单和 Codex manifest 配置，避免普通分组 Key 行为退化。

关键文件：`be/service/api_key_service.go`、`be/service/api_key.go`（`IsGlobal()`）、`be/service/api_key_auth_cache.go`、`api_key_auth_cache_impl.go`、`be/service/gateway_scheduling.go`、`be/repository/api_key_repo.go`、`be/handler/api_key_handler.go`、`be/handler/global_key_routing.go`（`resolveGlobalAPIKeyForModel`）、`be/service/auth_service.go`。Ent schema 在 `backend/ent/schema/api_key.go`。前端：`fe/views/user/KeysView.vue`、`fe/api/keys.ts`、`fe/types/index.ts`。契约：`backend/dev-docs/tokenpro-global-key-contract.md`。

上游一改 `api_keys` 的 Ent schema，必须重新运行 `go generate ./ent`，不能手工合并生成代码。鉴权缓存加字段时要同步保留 `key_type`。

## 2. Codex 直连模型路由（tp-g / X-TokenPro-Group-Id）

Codex 客户端把所选分组编进模型 slug，例如明文 `tp-g57-gpt-5.6-luna` 或旧版 Base64 `tp-g57-Z3B0...`。入口中间件在白名单校验、调度、转发、计费之前还原公开模型名，并注入请求级分组。同名模型不跨组猜测；分组无效时直接拒绝。旧客户端请求头 `X-TokenPro-Group-Id` 继续兼容。

常量 `tp-g` 与 `X-TokenPro-Group-Id` 定义在 `be/server/middleware/tokenpro_direct_route.go`。相关文件：`be/server/middleware/middleware.go`、`be/server/router.go`、`be/handler/global_key_routing.go`、`be/handler/openai_codex_models_handler.go`、`be/service/openai_codex_transform.go`。响应中的模型名回写在 `openai_gateway_response_handling.go`，需要把 slug 改回公开模型名。

## 3. 各网关入口的全局 Key 接入点

OpenAI、Codex、Claude、Gemini、Grok 等入口（HTTP、流式、WebSocket）都在调度前把全局 Key 解析成命中分组的 Key。每个文件通常是 1～3 处 `if apiKey.IsGlobal() { resolveGlobalAPIKeyForModel(...) }`：

- `be/handler/gateway_handler.go`
- `be/handler/gateway_handler_chat_completions.go`
- `be/handler/gateway_handler_responses.go`
- `be/handler/gateway_helper.go`
- `be/handler/gateway_web_search.go`
- `be/handler/openai_gateway_handler.go`
- `be/handler/openai_chat_completions.go`
- `be/handler/openai_embeddings.go`
- `be/handler/openai_gateway_count_tokens.go`
- `be/handler/openai_live.go`
- `be/handler/openai_alpha_search.go`
- `be/handler/openai_images.go`
- `be/handler/grok_audio.go`
- `be/handler/grok_media.go`
- `be/handler/gemini_v1beta_handler.go`
- 路由层 `be/server/routes/gateway.go`（`resolveGlobalRouteKey`：先读 body 里的 model 再解析，图片路由交给 handler）
- 服务层 `be/service/openai_gateway_forward.go`、`openai_gateway_passthrough.go`、`openai_gateway_request_body.go`、`openai_gateway_response_handling.go`

上游每新增一个入口 handler 或路由，都要人工检查是否需要加全局 Key 解析。可以用 `rg -n 'apiKey.GroupID|GetAPIKeyFromContext' backend/internal/handler` 列出未接入的位置。

### Gemini 原生端点（已确认保留）

全局 Key **允许**调用 Gemini 原生 `/v1beta/models...` 端点。v0.2.17 在同一位置新增了 composite 分组判断（`servesCompositeAntigravityGemini`）。合并顺序固定为：

1. 平台校验加上 `!apiKey.IsGlobal()`，全局 Key 跳过“分组平台必须是 gemini”的检查；
2. 执行 `resolveGlobalAPIKeyForModel`；
3. 重新计算 `effectivePlatform = effectiveAPIKeyPlatform(c, apiKey)`；
4. 再执行上游的 composite / Antigravity 模型检查。

`ListModels` 对全局 Key 返回内置目录 `gemini.FallbackModelsList()`，不按空分组去上游查询。Gemini 显式缓存管理遵循上游约束：只允许固定 Gemini 分组 Key，不为全局 Key 猜测缓存归属。

## 4. 生图桥接 / 文本组生图交付 / 本地标题

Codex OAuth/SetupToken 账号的原生图片接口独立于文本模型映射。文本分组里的图片工具调用由 TokenPro 完成授权、轮次、回执和交付。桌面端本地生成会话标题，不走上游计费。

关键文件（大多为新增）：`be/service/tokenpro_native_dispatch.go`、`tokenpro_native_images.go`、`tokenpro_image_receipt.go`、`tokenpro_image_reply.go`、`tokenpro_image_turn.go`、`tokenpro_images_authorization.go`、`tokenpro_text_image_tools.go`、`tokenpro_local_title.go`；`be/repository/tokenpro_image_receipt.go`、`tokenpro_image_turn.go`；`be/handler/tokenpro_native_dispatch.go`。接入点包括 `be/service/openai_images.go`、`openai_images_responses.go`、`be/handler/openai_images.go`、`be/service/account.go`（`IsOpenAIOAuthLike() && IsGPTImageGenerationModel` 直接放行）、`be/server/routes/gateway.go`（`imageTurnStores`）、`be/handler/wire.go`、`backend/cmd/server/wire_gen.go`。文档：`backend/dev-docs/tokenpro-image-delivery-latency.md`、`docs/TOKENPRO_R54_NATIVE_IMAGES.md`。

图片权限由分组 `allow_image_generation` 控制。上游改 wire 依赖时要重新生成 `cmd/server/wire_gen.go`，不要手工拼接。

账号 Extra 开关 `openai_response_model_rewrite_enabled`（`be/service/account.go`）控制 OpenAI 响应模型名回写，与全局 Key 路由一起保留。

## 5. 支付与计费二开

- 支付渠道配置、渠道限额、固定手续费、到账倍率、订阅倍率、币种名称、负载均衡。
- 订单保存渠道快照；旧的全局商业配置和产品名一次性迁移到渠道。
- 保留 TokenPro 结果页和 Stripe 手动返回。

关键文件：`be/service/payment_order.go`、`payment_order_provider_snapshot.go`、`payment_config_{limits,providers,service}.go`、`payment_fulfillment.go`、`payment_service.go`、`payment_provider_commercial_migration.go`、`payment_provider_product_name_migration.go`、`be/payment/fee.go`、`load_balancer.go`、`be/handler/payment_handler.go`、`be/handler/admin/payment_handler.go`。前端：`fe/views/user/PaymentView.vue`、`StripePaymentView.vue`、`PaymentResultView.vue`、`fe/components/payment/`。后台旧全局商业设置用 `v-if="false"` 隐藏。

v0.2.17 重写了退款（待定期间保留扣减，网关失败后原子回退）、易支付查单，并在回调确认后才入账；下单校验订单类型。这些与渠道快照、固定手续费的组合需要在升级后回归。

## 6. 安全审计与内容监控

- 提示词审计配置、持久化、事件入队、处理和查询扩展。
- 内容监控后台可单独监控最多 100 个邮箱，配置项 `monitor_user_emails`（`be/securityaudit/prompt_config.go`，超限返回 `prompt_audit_too_many_monitor_users`）。
- 审计日志补充请求信息。内容监控里只显示被监控用户的输入（`prompt_monitor_input.go`）。

前端：`fe/features/content-monitor/`、`fe/features/prompt-audit/`、对应中英文案、路由和侧栏菜单。

## 7. 用户、认证、桌面登录票据、注册 IP

- 记录用户注册 IP，并在后台用户列表展示。迁移 `196_add_user_created_ip.sql`。
- 桌面客户端登录票据：`fe/views/auth/DesktopAuthView.vue`，配合 `be/service/auth_service.go`、`be/handler/auth_handler.go`、`be/server/routes/auth.go`、refresh token 缓存。
- 登录协议提示：`fe/components/auth/LoginAgreementPrompt.vue`。
- 本地标题见第 4 节，不作为计费请求。

`users` 的 Ent schema 有冲突时先合 schema，再重新生成 Ent 代码。注册流程同时挂着全局 Key 补齐。

## 8. 模型广场

显示人民币实付价并与官方价对比、排序，接入上游独立视频倍率。文件：`be/service/model_plaza_service.go`、`fe/components/modelPlaza/PlazaModelPricingTable.vue`。

## 9. 品牌、文档与部署

- TokenPro 版本号显示 `TokenPro-Rxx-v0.2.x`，以及上游版本映射 `tokenProUpstreamVersion`。
- 管理员登录后检查底层新版本；同一浏览器会话对同一版本只提醒一次。
- Agent 使用文档页面、菜单、样式和中英文内容。
- 低内存 Docker 构建文件 `deploy/Dockerfile.lowmem`（Go 1.27.2，需与 `go.mod` 一致）。
- CI 工作流 `.github/workflows/tokenpro-server-image.yml` 推送镜像到 ghcr.io，使用 `GITHUB_TOKEN`，需要 packages 写权限。
- 服务器配置沿用 `deploy/config.example.yaml`。v0.2.17 新增可选项 `gateway.min_output_tokens`，默认 128。

Ent 生成代码和 `wire_gen.go` 必须通过工具重新生成，不能在冲突里手工拼接。

## 10. 分组与调度兼容

- 保留普通分组 Key 的创建、编辑、删除、固定分组调用行为。
- 全局 Key 动态选择时过滤禁用、过期、无账号或不可调度的分组。
- 使用实际命中的分组和账号进入后续调度、使用记录与计费链路。
- 合并上游分组模型白名单；WebSocket 请求先解析全局 Key，再校验命中分组的白名单。
- 全局 Key 在用户列表中固定置底；隐藏手动使用、复制、CCS 导入、启停和编辑入口，仅保留重置。后端拒绝普通更新接口修改。

## 11. 数据库变更

Fork 自有迁移（按完整文件名执行，与上游同编号文件不冲突；不要改名，也不要改动旧迁移内容）：

- `196_add_user_created_ip.sql`：用户注册 IP。
- `234_api_key_scope.sql`：`api_keys.key_type`（group/global）、每用户唯一全局 Key 的索引、CHECK 约束、存量用户补齐。
- `237_global_api_key_repair.sql`：幂等补齐曾被旧版本删除或遗漏的全局 Key，历史软删除记录保留用于审计。
- `238_global_api_key_group_invariant.sql`：强制全局 Key 的 `group_id` 为空。

上游同编号文件包括 `196_channel_monitor_v2_...`、`234_channel_max_...`、`234_group_codex_...`、`237_add_minimax_platform`、`238_opencode_go_platform`、`238_purge_unlimited_...`。今后新增 fork 迁移建议使用 `9xx_tokenpro_*.sql`，并先确认执行器排序规则。v0.2.16 到 v0.2.17 上游没有新增迁移。

## 12. 已删除的遗留备份

R83 升级删除了以下误提交的备份文件，后续不要再提交 `*.before-*`：

- `fe/components/layout/AppHeader.vue.before-menu-label-20260905`
- `fe/i18n/locales/en/common.ts.before-menu-label-20260905`
- `fe/i18n/locales/zh/common.ts.before-menu-label-20260905`
- `fe/i18n/locales/en/misc.ts.before-operation-complete-20260905`
- `fe/i18n/locales/zh/misc.ts.before-operation-complete-20260905`
- `fe/views/user/StripePaymentView.vue.before-operation-complete-20260905`
- `fe/views/user/StripePaymentView.vue.before-payment-guard-20260905`

## 后续升级约定

1. 只合并上游正式标签，使用 `git merge`，不用 rebase。先读 `tokenProUpstreamVersion` 和 `VERSION`，确认上一个底层标签是历史合并提交的第二父提交。
2. 冲突优先同时保留上游修复和上述 TokenPro 行为，不以整文件覆盖。Gemini 与平台校验类冲突按第 3 节的四步顺序处理。
3. schema 冲突时先合 `ent/schema/*.go`，再 `go generate ./ent`；wire 变更后重新生成 `cmd/server/wire_gen.go`，然后 `go mod tidy`。
4. 每次升级同步 `tokenProUpstreamVersion` 与 `backend/cmd/server/VERSION`，并让 `deploy/Dockerfile.lowmem` 的 Go 版本与 `go.mod` 一致。
5. 检查上游新增迁移是否改动 `api_keys`、`users` 或支付相关表，是否与 234/237/238 的约束冲突。旧迁移一律不改。
6. 上线前至少执行后端 `go build` / `go vet` / `go test`、前端类型检查与 vitest、数据库迁移检查，以及全局 Key 的并发、流式和普通分组 Key 回归测试。

## 升级记录

### v0.2.16（TokenPro-R78）

- 基座固定为上游正式标签 `v0.2.16`，保留 TokenPro 全局密钥、Codex 路由与生图桥接、支付渠道定价、审计、品牌和桌面登录票据。
- 充值优惠沿用上游阶梯逻辑，但到账倍率、固定手续费与订阅倍率继续从所选渠道读取；优惠和渠道定价均写入订单快照。
- 模型广场保留人民币实付价与官方价格比较，接入上游独立视频倍率。
- Stripe 成功后保留 TokenPro 结果页及手动返回；离开页面不再更新状态，轮询不重复并发。
- 低内存构建镜像同步升级至 Go 1.27.2。
- 旧 SQL 迁移保持原内容，新增上游迁移按文件名自动执行。
- Gemini 显式缓存管理遵循上游固定 Gemini 分组密钥约束，不为全局密钥猜测缓存归属。
- 上游 v0.2.11 默认开启 API Key 创建限制与余额在途预占；v0.2.12 升级前发出的未使用重置密码链接会失效。
- 补齐 `upgrade/sub2api-v0.2.11-tokenpro`（R77）的嵌入版本解析、文本组图片驱动选择、Codex OAuth/SetupToken 图片能力独立于文本映射、完整图片工具响应生命周期四项修复。

### R79～R82（仍基于 v0.2.16）

- **R79**（PR #2）：统一全局 Key 生图与本地桌面标题。
- **R80**（PR #3）：全局文本路由遵循 Codex 图片工具能力。
- **R81**（PR #4）：文本路由图片授权，内容监控只展示被监控用户的输入。
- **R82**（PR #5）：降低文本组生图交付的轮询开销。发行名为 `TokenPro-R82-v0.2.16`。

### v0.2.17 升级记录（TokenPro-R83）

R83 已在生产环境运行。本分支用合并提交把 `main`（`9ed03e9`，R82）升到上游标签 `v0.2.17`（`060d016`），不 rebase。三个冲突的处理：

- `be/handler/gemini_v1beta_handler.go`：按第 3 节四步保留全局 Key 调用 Gemini 原生端点；模型列表对全局 Key 返回内置目录。
- `backend/cmd/server/VERSION`：`TokenPro-R83-v0.2.17`。
- `be/service/gateway_forward_as_responses.go`：fork 侧只删过空行，采用上游版本。

同时把 `tokenProUpstreamVersion` 改为 `0.2.17`，并删除第 12 节列出的 7 个备份文件。没有重新生成 Ent 或 wire：本次上游没有改到 fork 的 schema 冲突。

影响 TokenPro 的上游行为：

- **GPT-5.5 订阅退役**：ChatGPT OAuth/SetupToken 账号不再调度和展示 `gpt-5.5`。全局 Key 若只能由这类账号承接该模型，会返回“当前模型没有可用分组或有效订阅。”。
- **支付退款重写**：退款待定期间保留余额和订阅扣减，网关确认失败后原子回退；支付宝退款可查询状态；易支付查单修复；回调确认后才入账；下单校验订单类型。需与渠道快照、固定手续费一起验证。
- **Stripe `client_secret` 不再经 URL 传递**：TokenPro 支付页本来就不在 URL 里传它，已兼容。
- **composite 分组 `/v1beta`**：与全局 Key 的 Gemini 决策在同一处代码，按第 3 节顺序合并。
- **OpenAI API Key 账号的 Web Search 模拟，以及 count_tokens 使用 Anthropic base URL**：这些路径仍先解析全局 Key。
- 新增配置 `gateway.min_output_tokens`（默认 128，最低 16）。
