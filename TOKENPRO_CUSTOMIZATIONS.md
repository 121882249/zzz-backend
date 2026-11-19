# TokenPro 二次开发清单

当前分支以 Sub2API `v0.2.23`（`5af9afb`，tag 对象 `ce19a64`）为底层版本。构建版本是 `0.2.23-tp.1`：`backend/cmd/server/VERSION` 为 `0.2.23-tp.1`，`backend/internal/service/update_service.go` 中的 `tokenProUpstreamVersion` 为 `0.2.23`。管理端按数字核心比较上游，`0.2.23-tp.N` 与上游 `v0.2.23` 相同，不会提示可更新。此文件用于后续升级时区分上游改动和本项目改动。已部署回滚点仍是 `619e567b`（`0.2.19-tp.1`），在分支 `upgrade/sub2api-v0.2.18-tokenpro`，没有被改写。`0.2.22-tp.6`（`6b3f191`）和更早的生产包没有被改写。

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

`ListModels` 对全局 Key 返回内置目录 `gemini.FallbackModelsList()`，不按空分组去上游查询。未携带分组（没有 `tp-g` slug，也没有 `X-TokenPro-Group-Id`）时与其他入口一样，经 `respondGlobalKeyRoutingError` 返回 HTTP 400，错误码 `group_id_required`，响应仍是 Gemini 错误体。分组无效仍返回 503；平台不匹配仍返回 400。Gemini 显式缓存管理遵循上游约束：只允许固定 Gemini 分组 Key，不为全局 Key 猜测缓存归属。

## 4. 生图桥接 / 文本组生图交付 / 本地标题

Codex OAuth/SetupToken 账号的原生图片接口独立于文本模型映射。文本分组里的图片工具调用由 TokenPro 完成授权、轮次、回执和交付。桌面端本地生成会话标题，不走上游计费。

关键文件（大多为新增）：`be/service/tokenpro_native_dispatch.go`、`tokenpro_native_images.go`、`tokenpro_image_receipt.go`、`tokenpro_image_reply.go`、`tokenpro_image_turn.go`、`tokenpro_images_authorization.go`、`tokenpro_text_image_tools.go`、`tokenpro_local_title.go`；`be/repository/tokenpro_image_receipt.go`、`tokenpro_image_turn.go`；`be/handler/tokenpro_native_dispatch.go`。接入点包括 `be/service/openai_images.go`、`openai_images_responses.go`、`be/handler/openai_images.go`、`be/service/account.go`（`IsOpenAIOAuthLike() && IsGPTImageGenerationModel` 直接放行）、`be/server/routes/gateway.go`（`imageTurnStores`）、`be/handler/wire.go`、`backend/cmd/server/wire_gen.go`。文档：`backend/dev-docs/tokenpro-image-delivery-latency.md`、`docs/TOKENPRO_R54_NATIVE_IMAGES.md`。

图片权限由分组 `allow_image_generation` 控制。上游改 wire 依赖时要重新生成 `cmd/server/wire_gen.go`，不要手工拼接。

当聊天模型本身是生图模型（分组 `description` 去空白后等于 `生图`），并且请求**没有**携带 `image_gen.imagegen` 工具时，后端直接生图。官方 Codex（User-Agent 或 originator 命中 `Codex Desktop`、`Codex CLI`、`codex_cli_rs`、`codex_vscode` 等）生图成功的首轮默认走 `data-v1`：assistant `message` 是 `图片已生成：`、空行、`![](data:image/jpeg;base64,…)`、空行、`[原图链接](https://…签名 URL…)`，有调整描述时再加一行。预览图长边约 1024px、JPEG 质量约 80，压到几百 KB 以内。解码或压缩失败，或 `gateway.tokenpro_codex_image_mode` 设为 `url-v1` 时，退回 `图片已生成：`、空行、`![](https://…)`、空行、`[https://…](https://…)`，响应头 `url-v1`。成功的 data-v1 响应头是 `X-TokenPro-Chat-Image: data-v1`。是否带 `X-TokenPro-Image-Mode: native-v2`、是否带 exec/shell，都不改这条。流式补齐 `output_item.added`、`content_part.added`、`output_text.delta`、`output_text.done`、`content_part.done`、`output_item.done`、`response.completed`，并且 `completed.output` 含这条 message。不回 `image_generation_call`，也不在首轮下发本机下载工具。转发 `/v1/responses` 和 `/v1/chat/completions` 之前，把历史里我们嵌进去的 `![…](data:image/…;base64,…)` 换成 `[已生成图片]`，旁边的签名链接留下；用户自己的 `input_image` 不动。其他客户端不变：有 shell/exec 时下发本机下载（`local-v1` 的 `function_call` 或 `custom_tool_call`，打印 `TOKENPRO_IMAGE_PATH`）；没有这些工具时 assistant 文本是 `图片已生成：https://tokenpro.work/...`；显式 `X-TokenPro-Image-Mode: native-v2` 才返回内联 `image_generation_call`（标准 base64，`status=completed`，响应头 `inline-v1`）。本机下载的后续轮次如果解析到允许目录下的绝对路径，助手文本是 `![](localpath)`；解析不到时改为 markdown 图片加一行可点击链接，并记录不含密钥、URL 和图片数据的原因码。请求工具里真有 `image_gen.imagegen`，或历史里已有 `call_tp_pure_` / `call_tp_receipt_` 时，仍走原来的 native-v1 function call，不在这里生图。生图组按单图计费一次，下载回执轮次不重复计费。错误请求不扣费。签名下载 `GET` 和 `HEAD /v1/tokenpro/images/:id` 都返回同一张原图。

全局 key 的 GPT 文本组（不是 `生图` 分组）把文本模型生图交回上游 v0.2.22：官方 Codex、分组允许生图、生图桥开关和显式工具策略决定是否注入 `image_generation`。透传账号不注入。上游的 `image_generation_call` 和 partial 事件原样转发，不改写成 data-v1、url-v1 或本机下载，也不落盘。imagegen skill 目录不再改写。这类请求也不再把过长的 `image_generation_call.result` 清空。计费仍是文本 token 一次加上恰好一次 gpt-image-2 按张费用，按上游实际出图张数判断，不依赖改写器；后续轮没有新图就不收图片费，错误不扣费。`tp-g` 路由保留。普通分组 key 仍走原来的改写。生图组的 data-v1 / url-v1 不变。历史里生图组嵌的 `![…](data:image/…;base64,…)` 在文本组请求里仍会换成 `[已生成图片]`，避免把预览字节转发出去。

其他客户端带 `X-TokenPro-Image-Mode: native-v2` 时，Responses 流式和非流式都保留 `image_generation_call`，`result` 是标准 base64，终态 `status` 为 `completed`，日志是 `Rewrote hosted image_generation_call as inline-v1`。不带这个头的其他客户端，若不是上面的全局 key 文本组，仍走本机下载或签名链接。

链接优先来自已启用的 `image_storage`（必须是 HTTPS，公开域名或 presigned 都可以）。没有可用对象存储时，使用签名下载：

- `gateway.tokenpro_image_public_base_url`：公网 HTTPS 源站，例如 `https://tokenpro.work`，不要带 `/v1`。
- `gateway.tokenpro_image_url_secret`：HMAC 密钥，重启和多副本必须相同。
- `gateway.tokenpro_image_url_ttl_hours`：有效期，`0` 表示 7 天。
- `gateway.tokenpro_image_store_dir`：本地文件目录，空值是 `data/tokenpro-chat-images`。
- `gateway.tokenpro_chat_image_keepalive_seconds`：生图 SSE 的 `response.in_progress` 间隔，`0` 表示 10 秒。
- `gateway.tokenpro_codex_image_mode`：官方 Codex 首轮预览，默认 `data-v1`。设成 `url-v1` 就只发 HTTPS markdown 链接。

下载路由是 `GET /v1/tokenpro/images/:id?exp=&sig=`，注册在 API Key 鉴权之外，签名错误返回 404，过期返回 410 并删除文件。两项存储都没配好时返回中文配置错误，不回传 data URI。示例见 `deploy/config.example.yaml`。

账号 Extra 开关 `openai_response_model_rewrite_enabled`（`be/service/account.go`）控制 OpenAI 响应模型名回写，与全局 Key 路由一起保留。

## 5. 支付与计费二开

- 支付渠道配置、渠道限额、固定手续费、到账倍率、订阅倍率、币种名称、负载均衡。
- 订单保存渠道快照；旧的全局商业配置和产品名一次性迁移到渠道。
- 保留 TokenPro 结果页和 Stripe 手动返回。
- 充值账户卡把「大额充值私聊站长 优惠」（`payment.largeRechargeHint`）放在「当前余额」同一行的最右侧；窄屏换行后提示仍靠右。后台充值赠送公告和帮助文案仍在各自卡片里，不替代这句。

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

- 构建版本号是 semver `X.Y.Z-tp.N`（当前 `0.2.23-tp.1`）。管理端徽章对非 `TokenPro-` 前缀补一个 `v`，所以界面显示 `v0.2.23-tp.1`。上游是否可更新只比较 `tokenProUpstreamVersion` 解析出的 `X.Y.Z`。
- 管理员登录后检查底层新版本；同一浏览器会话对同一版本只提醒一次。
- Agent 使用文档页面、菜单、样式和中英文内容。
- 低内存 Docker 构建文件 `deploy/Dockerfile.lowmem`（Go 1.27.4，需与 `go.mod` 一致）。
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

上游同编号文件包括 `196_channel_monitor_v2_...`、`234_channel_max_...`、`234_group_codex_...`、`237_add_minimax_platform`、`238_opencode_go_platform`、`238_purge_unlimited_...`。今后新增 fork 迁移建议使用 `9xx_tokenpro_*.sql`，并先确认执行器排序规则。v0.2.16 到 v0.2.17 上游没有新增迁移。v0.2.18 新增 `246_enable_gemini_image_generation_groups.sql`：把已有 `gemini` / `antigravity` 分组的 `allow_image_generation` 置为 true。`composite` 不在该 UPDATE 里。v0.2.19 在它之后按文件名执行 `247_scoped_admin_api_keys.sql`（管理员 API Key 表）和 `248_group_image_count_billing.sql`（`groups.image_count_billing`，默认 false）。248 不改 246 的开关，也不改 `api_keys`、`users` 或支付表。v0.2.20 新增 `249_mcp_oauth.sql`，只建 `mcp_oauth_grants` / `mcp_oauth_codes` / `mcp_oauth_credentials`，`CREATE TABLE IF NOT EXISTS`，不改 `api_keys`、`users` 列或支付表。回滚二进制到 `0.2.19-tp.1` 时这些表可以留下；只有确认从未启用 MCP 时才按外键逆序删除这三张表。v0.2.21 再新增两个上游文件，按文件名排在 `249_mcp_oauth.sql` 前后：`249_add_usage_logs_api_key_cost_index_notx.sql`（`usage_logs` 上 `CREATE INDEX CONCURRENTLY`，覆盖 `(api_key_id, created_at) INCLUDE (actual_cost)`，不改列、不锁表）和 `250_openai_cache_write_ttl.sql`（`usage_logs` 增加 `cache_creation_30m_tokens` / `cache_creation_12h_tokens`，渠道与账号统计价卡增加可空的 `cache_write_12h_price`）。二者都不改 `api_keys`、`users` 或支付订单表。执行器按文件名跳过已应用记录，所以已跑过 `249_mcp_oauth.sql` 的库仍会补跑索引和 250。v0.2.22 再新增两个上游文件，都编号 251，按文件名排在 250 之后，与 fork 已有迁移不重名：`251_group_force_openai_ultrafast.sql`（`groups` 增加 `force_openai_ultrafast`，默认 false）和 `251_group_usage_rollup_nonblocking_build.sql`（`usage_group_rollup_state` 增加重建水位列，并替换失效函数）。二者都不改 `api_keys`、`users` 或支付订单表，也不改 234/237/238 的全局 Key 约束。执行顺序是 `251_group_force_openai_ultrafast.sql`，然后 `251_group_usage_rollup_nonblocking_build.sql`。v0.2.23 新增 `252_channel_pricing_ultrafast_multiplier.sql`：`channel_model_pricing` 增加可空的 `ultrafast_multiplier`，并加正数 CHECK。不改 `api_keys`、`users`、`groups` 或支付订单表，也不改 234/237/238。合并前共 308 个 SQL 文件，没有同名或同编号 `252_` 文件。按文件名排在两个 `251_` 之后。

## 12. 已删除的遗留备份

R83 升级删除了以下误提交的备份文件，后续不要再提交 `*.before-*`：

- `fe/components/layout/AppHeader.vue.before-menu-label-20260905`
- `fe/i18n/locales/en/common.ts.before-menu-label-20260905`
- `fe/i18n/locales/zh/common.ts.before-menu-label-20260905`
- `fe/i18n/locales/en/misc.ts.before-operation-complete-20260905`
- `fe/i18n/locales/zh/misc.ts.before-operation-complete-20260905`
- `fe/views/user/StripePaymentView.vue.before-operation-complete-20260905`
- `fe/views/user/StripePaymentView.vue.before-payment-guard-20260905`

## 构建版本号

生产包曾经把 `PR7-a34e450` 写进 `-X main.Version`。管理端显示 `vPR7-a34e450`，更新检查把它解析成 `0.0.0`，于是基于上游 `v0.2.17` 的构建也被提示“有新版本 v0.2.17”。

版本从这三处进入二进制，后者覆盖前者：

1. `backend/cmd/server/VERSION`，由 `backend/cmd/server/main.go` 的 `//go:embed VERSION` 打进二进制。
2. 精确 git 标签（`vX.Y.Z`、`X.Y.Z-tp.N` 或旧的 `TokenPro-R<N>-vX.Y.Z`）。`backend/scripts/resolve-version.sh` 会去掉一个前导 `v`。
3. 编译参数 `-X main.Version=`。Docker 的 `VERSION` 构建参数走这个路径。根目录 `Dockerfile`、`backend/Dockerfile`、`deploy/Dockerfile`、`deploy/Dockerfile.lowmem` 都调用 `./scripts/resolve-version.sh "${VERSION:-}"`。

脚本只接受 `X.Y.Z`、`X.Y.Z-tp.N` 和 `TokenPro-R<N>-vX.Y.Z`。`PR7-a34e450` 这类标签会被丢掉，然后改用 VERSION 文件；文件本身也不合法时构建失败。镜像 CI（`.github/workflows/tokenpro-server-image.yml`）用同一规则校验 VERSION 文件，再把它作为 `VERSION` 构建参数传入。

以后这样改版本：

- 仍基于当前上游、只发 TokenPro 改动：把 `backend/cmd/server/VERSION` 的 `tp.N` 加一，例如 `0.2.21-tp.3`。不要改 `tokenProUpstreamVersion`。
- 合并新的上游正式标签：把 `tokenProUpstreamVersion` 改成该标签的 `X.Y.Z`，并把 VERSION 文件写成 `X.Y.Z-tp.1`。
- 手工或 CI 构建时，`VERSION` 构建参数必须是上述三种格式之一，并且和 VERSION 文件一致。不要再传入 PR 号或短 SHA。
- 更新检查比较的是数字核心，不把 `-tp.N` 当成预发布。`0.2.21-tp.1` 等于上游 `0.2.21`。无法解析的当前版本按 `tokenProUpstreamVersion` 比较，不再当成 `0.0.0`。旧发行名 `TokenPro-R28`（没有 `-vX.Y.Z`）仍回落到这个常量。

本地确认：`backend/scripts/resolve-version.sh`、`backend/scripts/resolve-version.sh 0.2.20-tp.1` 应打印版本；`backend/scripts/resolve-version.sh PR7-a34e450` 应忽略参数并打印 VERSION 文件。

## 后续升级约定

1. 只合并上游正式标签，使用 `git merge`，不用 rebase。先读 `tokenProUpstreamVersion` 和 `VERSION`，确认上一个底层标签是历史合并提交的第二父提交。
2. 冲突优先同时保留上游修复和上述 TokenPro 行为，不以整文件覆盖。Gemini 与平台校验类冲突按第 3 节的四步顺序处理。
3. schema 冲突时先合 `ent/schema/*.go`，再 `go generate ./ent`；wire 变更后重新生成 `cmd/server/wire_gen.go`，然后 `go mod tidy`。
4. 每次升级按上一节同步 `tokenProUpstreamVersion` 与 `backend/cmd/server/VERSION`，并让 `deploy/Dockerfile.lowmem` 的 Go 版本与 `go.mod` 一致。
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

### v0.2.18 升级记录

本分支用合并提交把生产分支 `cursor/codex-desktop-inline-4391`（`89819a9`，尚未并入 main）升到上游标签 `v0.2.18`（`ac1b8f1`），不 rebase。三个冲突的处理：

- `backend/cmd/server/VERSION`：写成 `0.2.18-tp.1`，`tokenProUpstreamVersion` 改为 `0.2.18`。
- `frontend/package.json`：依赖与 override 采用上游 DOMPurify `3.4.17`；`follow-redirects` 继续钉死 TokenPro 已升级的 `1.16.1`。
- `frontend/pnpm-lock.yaml`：override 与 package.json 对齐，锁定 DOMPurify `3.4.17` 与 follow-redirects `1.16.1`。

其余双方都改过的文件由 git 自动合并，并核对过网关与生图门禁：

- Gemini `/v1beta` 仍按第 3 节四步处理全局 Key。新的 `allow_image_generation` 检查在 `resolveGlobalAPIKeyForModel` 之后，用解析出的分组，不看解析前的空分组。
- OpenAI 分组不进入 Gemini 门禁。全局 Key 文本模型的托管生图（注入 `image_generation`、`output_format=png`、local-v1 下载路径、技能目录中和、标题请求不生图不计费）保持原样。
- `openai_gateway_usage.go` 的托管生图计费未被上游改动：`ImageCount>0 && isOpenAIImageGenerationModel(result.BillingModel)` 与 `billing_model_source` 无关；按次费走纯图片价乘 `resolveImageRateMultiplier`，只加一次；文本部分用 token 倍率；图片输出 token 清零。Gemini 等非 GPT 生图模型继续按次，除非渠道是 token 模式。

没有重新生成 Ent 或 wire：上游没有改到 fork 的 schema。Go 工具链两边都已是 1.27.3，`deploy/Dockerfile.lowmem` 无需再改。

影响 TokenPro 的上游行为：

- **迁移 246**：已有 Gemini / Antigravity 分组的生图开关被置为开启。composite 分组不在这条 UPDATE 里，升级后保持原值；未开启的混合分组发起 Gemini 生图会返回 400，需要到分组设置里手动打开「允许当前分组生图」。同一开关本来就约束该分组上的 OpenAI 生图。
- **Grok 4.7 Fast**：内置价卡改为官方 Fast 价（标准 grok-4.7 的 2 倍：输入 $4 / 缓存 $1 / 输出 $12 每百万 token；满 200k 后 $6 / $1.50 / $18，即标准长上下文价的 1.5 倍）。`grok-4.7-fast` 与 `grok-4.7-build-fast` 都走这张卡。分组或渠道若单独写了模型价，仍以那张卡为准，需要核对是不是还停在旧的标准价。
- **异步生图转存**：下载拒绝内网、回环等非公网地址，含每次重定向；非图片内容直接失败。
- **流式计费**：Anthropic 兼容流客户端断开后读完上游再计费；上游未正常结束时返回协议内错误；Gemini 流式中断按已收集用量计费；prompt 被拦截且只报缓存命中时 input token 不再为负。
- **Codex 重试**：`response.queued` 与 `response.metadata` 不再算作已开始输出，随后的 overload 仍可安全重试。
- **Vertex service account** 支持总额/日/周配额上限。新建 Gemini / Antigravity 分组默认允许生图；新建 composite 分组默认关闭。

### v0.2.19 升级记录

本分支用合并提交把 `upgrade/sub2api-v0.2.18-tokenpro`（`1091152`）升到上游标签 `v0.2.19`（`dece261`），不 rebase。五个冲突的处理：

- `backend/cmd/server/VERSION`：写成 `0.2.19-tp.1`，`tokenProUpstreamVersion` 改为 `0.2.19`。
- `api_key_auth_cache_impl.go`：快照版本升到 26，同时保留 key 类型、模型白名单、Codex manifest，并写入 `image_count_billing`。
- `openai_gateway_usage.go`：托管文本生图仍先识别 `ImageCount>0 && isOpenAIImageGenerationModel(result.BillingModel)`。文本按 token 倍率计一次，gpt-image-2 按次费按图片倍率计一次。`image_count_billing` 只在这不是托管桥接、且请求来自专用图片端点时强制按张，避免把桥接行改成纯图片价或再加一笔图片费。渠道按次图片档把 token 用量传进档位匹配。
- `openai_images.go` 与 `openai_images_responses.go`：开关关闭时仍只按响应里真正交付的图片计费，不用请求的 `n` 补账。开关打开时走上游 `resolveBillableImageCount`（有图按张数，成功但数不出图则按 1 张）。

Gemini `/v1beta` 仍按第 3 节四步处理全局 Key，`allow_image_generation` 仍在解析分组之后。`X-TokenPro-Group-Id` 与 `tp-g` 路由未改。迁移按文件名排序，246、247、248 依次执行。Ent schema 由 git 合并（分组新增 `image_count_billing`，fork 的 `api_keys.key_type` 仍在），没有手工拼接生成代码。

影响 TokenPro 的上游行为：

- **按张计费默认关闭**。打开后只作用于 OpenAI / Composite 的专用图片端点。托管文本生图不走这条开关。
- **Gemini fileData**：上游用 `fileData` / `file_data` 回图时也计入图片张数。
- **渠道按次图片档**：尺寸档未命中时用 token 用量匹配上下文档，避免记成 $0。
- Gemini 默认模型列表去掉 2.0/2.5 和 3-pro-preview。仍要这些模型需在白名单或映射里自行配置。
- 管理员 API Key 是新表，与用户全局 Key 的 `key_type` 无关。

### v0.2.20 升级记录

新分支 `upgrade/sub2api-v0.2.20-tokenpro` 从已部署提交 `619e567b`（`0.2.19-tp.1`，分支 `upgrade/sub2api-v0.2.18-tokenpro`）拉出，用 merge 合入官方标签 `v0.2.20`（`dab829a`，tag 对象 `ec41309`，tagger `shaw <dev@sub2api.org>`）。不 rebase，不改写已部署提交。PR #8 和 PR #9 保持打开。

冲突处理：

- `VERSION` 写成 `0.2.20-tp.1`。
- 认证快照版本升到 27，带上 `require_privacy_set`，并保留 key 类型、白名单、Codex manifest 和 `image_count_billing`。
- 图片 handler 同时套上尺寸档位分流和 TokenPro 生图授权。真实图片模型仍由图片账号映射决定，`gpt-image-2` 只作为托管桥接的计费实现细节。
- 网关路由保留签名图片下载和 `TokenProImageTurnStore`，并把请求体内存预算传给 `RequestBodyLimit`。
- 路由同时注册 MCP（默认关闭）和原来的全局 Key 网关。
- 示例配置保留 TokenPro 图片链接配置，SSE 单行上限改为上游默认 500MiB。
- 前端依赖采用 `follow-redirects >= 1.16.2` 和 `decode-uri-component >= 0.5.1`。侧栏测试同时保留模型广场和 MCP 入口。

请求体预算的生效键是 `gateway.request_body_memory_budget_bytes`（默认 1GiB）、`gateway.request_body_read_timeout_seconds` 和 `gateway.downstream_write_timeout_seconds`（默认都是 120，0 表示关闭）。发行说明写的 `security.request_body_*` 不会被读取。MCP 只有 `mcp.public_url` / `MCP_PUBLIC_URL` 非空才启用，默认空。

客户端断开后的上游 drain 有上限（默认跟随流间隔 180 秒，OpenAI 图片 API Key/OAuth 转发 900 秒）。窗口结束只结算已经观察到的用量。托管文本生图的计费公式没有改：文本一次，图像费一次。

### v0.2.21 升级记录

新分支 `upgrade/sub2api-v0.2.21-tokenpro` 从 `11e1c431`（`0.2.20-tp.2`，分支 `cursor/codex-inline-image-86e2`，PR #11）拉出，用 merge 合入官方标签 `v0.2.21`（提交 `10ba8f9`，tag 对象 `43f1529`，tagger `shaw <dev@sub2api.org>`）。不 rebase，不改写 `11e1c431` 和已部署回滚点 `619e567b`。

冲突处理：

- `VERSION` 写成 `0.2.21-tp.1`，`tokenProUpstreamVersion` 改为 `0.2.21`。
- `PaymentView.vue`：文案仍用 TokenPro 的 `{balance}`（「1 {currency} = {balance} USD 余额」），数值改走上游 `formatBalanceRechargeMultiplier`，最多 6 位小数。
- `KeysView.spec.ts`：表格桩同时渲染全局 Key 的 `cell-actions` 和上游新增的 `cell-usage`。
- `refresh_token_cache.go`：双方都加了原子消费方法和同名 Lua 脚本，合并后各出现两份。保留上游脚本和一份 `ConsumeRefreshToken`，空值判断用 `errors.Is`。桌面登录票据仍走原来的 `RefreshTokenConsumer`。

其余双方都改过的文件由 git 自动合并，并核对过：

- 托管文本生图计费仍是文本 token 一次，加上恰好一次 gpt-image-2 图像费。`image_count_billing` 不替换这一行，也不再加一笔。新的 30 分钟 / 12 小时缓存写入价加在同一条用量上，不改变图像费次数。
- `X-TokenPro-Image-Mode: native-v2` 仍在转发前删除。HTTP Responses 和 WebSocket 首帧都会清空过长的 `image_generation_call.result`。`partial_image` 仍不回给客户端。
- 全局 Key、`tp-g` / `X-TokenPro-Group-Id`、签名图片下载和请求体预算键名没有改。未显式配置时，预算改为内存上限的 1/4，最低 1GiB。
- Codex 页面上下文规范化发生在图片结果清空和全局 Key 解析之后，只改 `function_call_output`，不改 `image_generation_call`。
- 原生 Codex 生图请求（带 `X-Codex-Image-Turn-Id`）不再被 Images JSON 心跳提前写成 200。文本模型的托管桥接不走这条心跳。

没有重新生成 Ent 或 wire：`usage_log` schema 与生成代码由 git 合并，fork 的 `api_keys.key_type` 和 `groups.image_rate_*` 仍在。合并当时工具链是 1.27.3。随后 `govulncheck` 报标准库 `GO-2026-6859`（`net/http` 的 `MaxBytesReader` 头竞争，可打崩 HTTP/1 服务），修复版本是 Go 1.27.4。`go.mod`、CI 版本检查、根目录与 `deploy/`、`backend/` 的 Dockerfile 都改到 1.27.4，应用代码没动。

### v0.2.22 升级记录

新分支 `upgrade/sub2api-v0.2.22-tokenpro` 从 `e9ad59d7`（`0.2.21-tp.3`，分支 `cursor/codex-desktop-inline-image-9d19`，含 PR #12/#13/#14）拉出，用 merge 合入官方标签 `v0.2.22`（提交 `b0fa9f3`，tag 对象 `b11573c`，tagger `shaw <dev@sub2api.org>`）。不 rebase，不跟上游 main，不改写 `e9ad59d7` 和已部署回滚点 `619e567b`。

冲突处理：

- `VERSION` 写成 `0.2.22-tp.1`，`tokenProUpstreamVersion` 改为 `0.2.22`。上游该文件是 `0.2.21`。
- `api_key_auth_cache_impl.go`：快照版本升到 28。带上分组 `force_openai_ultrafast`，并保留 key 类型、模型白名单、Codex manifest、`image_count_billing` 和 `require_privacy_set`。

其余双方都改过的文件由 git 自动合并。`groups` schema 增加 `force_openai_ultrafast` 后重新运行 `go generate ./ent`；wire 重新生成，把 `UsageBillingLaneCombiner` 接成各服务使用的 `UsageBillingRepository`，关闭时 `Stop`。fork 的 `api_keys.key_type` 和分组图片价字段仍在。Go 工具链两边都是 1.27.4。

核对过的 TokenPro 行为：

- 合并当时，官方 Codex（Desktop / CLI / `codex_cli_rs`，UA 或 originator）的 Responses 流式和非流式返回内联 `image_generation_call`（`status=completed`，base64 `result`），响应头 `X-TokenPro-Chat-Image: inline-v1`。`0.2.22-tp.2` 已撤销这条按客户端身份自动内联的路径，官方 Codex 回到本机下载；`X-TokenPro-Image-Mode: native-v2` 仍走内联形状。公开模型名回写、过长 `result` 清空、全局 Key 和 `tp-g` 路由没有改。
- 文本模型生图仍是文本 token 一次，加上恰好一次 gpt-image-2 图像费。生图分组按张计费，一次请求里的多张图合成一条命令，每张图一份费用；下载回执轮次不记第二笔。4xx/5xx 不进入计费通道。
- 同一用户的扣费改经 `UsageBillingLaneCombiner` 合并进一个事务。合并只改变结算批次，不把两条命令的金额加总后再各扣一次，也不把图像费乘到文本行上。批量生图的冻结、结算、释放仍直接打到底层仓储。
- 充值页「大额充值私聊站长 优惠」仍在余额行最右侧（`payment.largeRechargeHint`）。
- 迁移 `251_group_force_openai_ultrafast.sql` 与 `251_group_usage_rollup_nonblocking_build.sql` 原样保留。fork 此前最高编号是 250，没有同名文件。

影响 TokenPro 的上游行为：

- **分组强制 Ultra Fast**：OpenAI / Composite 分组可强制 `service_tier=ultrafast`，只对 GPT-6 Astra 和 GPT-6.1 Sol 生效，优先级是 Ultra Fast > Fast > 客户端。6 倍费率加在这些模型的 token 价上，不加在 gpt-image-2 的按张费用上。
- **Vertex us/eu 多区域**：默认定价下发往多区域的 Gemini token 费用按 1.1 倍计；分组或渠道自定义价不再叠加。按次图片费不受这个倍数影响。
- **同一用户扣费合并**：高并发时一个用户的结算排成一条通道。单条命令的幂等键仍是 `request_id`。通道停掉之后的新命令直接打到底层仓储。
- 新增 Gemini 4 Carbon / Hydrogen 价卡，以及硅基流动 Qwen3 Embedding / Reranker 回退价。未知档位仍不计价。

### 0.2.22-tp.2 官方 Codex 内联生图回退

`0.2.22-tp.2` 只撤销 PR #14（`0.2.21-tp.3`，`ac6771d82`）给官方 Codex 加的内联 `image_generation_call` 交付。`tokenProUpstreamVersion` 仍是 `0.2.22`。上游 v0.2.22 的功能与迁移、计费规则、充值页「大额充值私聊站长 优惠」都保留。`0.2.22-tp.1`（`ac4b3b2`）没有被改写。

- 官方 Codex（Desktop / CLI / `codex_cli_rs`，按 User-Agent 或 originator）不再收到内联 `image_generation_call`。响应头不再是 `X-TokenPro-Chat-Image: inline-v1`，日志不再写 `Rewrote hosted image_generation_call as inline-v1`。
- 这些客户端和其他客户端一样走 local：`mode=local` 的 `function_call` / `custom_tool_call` 请客户端下载并打印 `TOKENPRO_IMAGE_PATH`。跟进轮次解析到允许目录下的绝对路径时返回 `![](localpath)`。解析不到时回退为 markdown `![](https://…)` 加一行可点击链接，并记录不含密钥、URL 和图片数据的原因码。没有 shell/exec 时仍是 `图片已生成：https://…`。
- `X-TokenPro-Image-Mode: native-v2` 仍在转发前删除，并继续把 Responses 里的 `image_generation_call` 以标准 base64、`status=completed` 交回。这条路径在 PR #14 之前就存在，没有撤掉。过长 `result` 清空也保留。
- 计费不变：文本模型生图是文本 token 一次，加上恰好一次 gpt-image-2 图像费。生图分组每张图计费一次，下载回执轮次不记第二笔。错误请求不进入计费。

### 0.2.22-tp.3 官方 Codex 生图改为可见链接

`0.2.22-tp.3` 修的是生产 `0.2.22-tp.2` 上官方 Codex Desktop 生图分组界面空白。`tokenProUpstreamVersion` 仍是 `0.2.22`。没有新迁移。计费、充值页提示和上游 v0.2.22 功能都保留。

原因：带 `X-TokenPro-Image-Mode: native-v2` 时只回 `image_generation_call`，codex-rs 的 `handle_non_tool_response_item` 不渲染它；不带这个头但有 exec 时，首轮只回 local 下载工具调用，对话里也没有图。

- 官方 Codex（Desktop / CLI / `codex_cli_rs`，按 User-Agent 或 originator）生图成功的首轮，无论是否带 native-v2、是否带 exec/shell，都返回 url 模式的 assistant `message`：`图片已生成：`、空行、`![](https://…)`、空行、`[https://…](https://…)`，可再加调整描述。响应头 `X-TokenPro-Chat-Image: url-v1`。流式事件含 `output_item.added`、`content_part.added`、`output_text.delta`、`output_text.done`、`content_part.done`、`output_item.done`、`response.completed`，`completed.output` 含这条 message。
- 生图分组（`tokenpro_chat_image`）和文本模型托管桥接（改写 hosted `image_generation_call`）都走这条。
- 非官方客户端不变：native-v2 仍内联，有 shell 仍本机下载，否则仍是纯链接文本。
- 计费不变：生图分组每张图一次；文本模型是文本 token 一次加恰好一次 gpt-image-2 图像费；跟进不重复计费；错误不扣费。

### 0.2.22-tp.4 官方 Codex 预览改为 data URI

`0.2.22-tp.4` 在 `0.2.22-tp.3` 上改官方 Codex 的首轮预览。没有新迁移。`tokenProUpstreamVersion` 仍是 `0.2.22`。

Codex Mac 0.169 能显示 tp.3 的文字和链接，但 webview 不去请求远程 `https://tokenpro.work/v1/tokenpro/images/…`，图片位置只剩占位图标。

- 默认 `data-v1`：`图片已生成：`、空行、`![](data:image/jpeg;base64,…)`、空行、`[原图链接](签名 URL)`。预览长边约 1024px，JPEG 质量约 80，不超过约 384KB。响应头 `X-TokenPro-Chat-Image: data-v1`。原图签名链接保留。
- 原图解码或压缩失败时退回 tp.3 的 `url-v1`。`gateway.tokenpro_codex_image_mode: url-v1` 也可以整段关掉 data URI。
- `/v1/responses` 和 `/v1/chat/completions` 转发上游前，把历史里的 `![…](data:image/…;base64,…)` 换成 `[已生成图片]`。`input_image` 不动。
- `HEAD /v1/tokenpro/images/:id` 与 GET 走同一签名下载，不再 404。
- 计费不变。非官方客户端不变。

### 0.2.22-tp.5 全局 key 文本组生图交回上游

`0.2.22-tp.5` 只改全局 key 的 GPT 文本组。生图组的 data-v1 / url-v1 不动。没有新迁移。`tokenProUpstreamVersion` 仍是 `0.2.22`。

- 是否注入 `image_generation` 只看上游条件：官方 Codex、分组 `allow_image_generation`、生图桥开关、显式工具策略。不再要求 UUID turn/thread，也不再改写 imagegen skill。透传账号不注入。
- 上游 `image_generation_call` 和 partial 事件原样转发，不挂 hosted writer，不落盘。
- 文本组不再清空过长的 `image_generation_call.result`。生图组嵌的 data URI markdown 仍换成 `[已生成图片]`。
- 计费仍按上游实际出图：文本 token 一次，加上恰好一次 gpt-image-2 按张费用。没有新图或请求失败就不收图片费。
- 普通分组 key 仍改写 hosted 图片。缺 `call_id` 的 400 校验没有改。

### 0.2.22-tp.6 文本组生图按实际图片模型准入

`0.2.22-tp.6` 在 `0.2.22-tp.5` 上修正全局 key 的文本组生图准入。没有新迁移。`tokenProUpstreamVersion` 仍是 `0.2.22`。

`X-TokenPro-Image-Route: tp-g16-…` 的 `/v1/images/generations` 仍然留在文本组，不改去生图组。实际图片模型是请求体里的 `model`；没有 `model` 时默认 `gpt-image-2`。这个模型已在该文本组的渠道白名单或分组可用模型里就原样放行。不在白名单里时，不直接拒绝，而是改写成该组白名单中的一个生图模型再转发：优先 `gpt-image-2`，否则按渠道定价列表的顺序，再按排序后的 `model_mapping` 键，最后按分组白名单顺序。计费、usage 和后续白名单检查都用改写后的模型，价格仍是这个文本组的价格。只有白名单里一个可用生图模型都没有时才拒绝，响应是「当前分组没有可用的生图模型」，不转发、不扣费。普通 key、生图组路由、data-v1 和 `/responses` 文本生图没有改。

### v0.2.23 升级记录

新分支 `upgrade/sub2api-v0.2.23-tokenpro` 从 `6b3f191`（`0.2.22-tp.6`，分支 `cursor/text-route-image-model-7f6b`，PR #20）拉出，用 merge 合入官方标签 `v0.2.23`（提交 `5af9afb`，tag 对象 `ce19a64`，tagger `shaw <dev@sub2api.org>`）。不 rebase，不改写 `6b3f191` 和已部署回滚点 `619e567b`。

冲突只有 `backend/cmd/server/VERSION`：写成 `0.2.23-tp.1`。上游该文件仍是 `0.2.22`。`tokenProUpstreamVersion` 改为 `0.2.23`。无法解析的构建号回落比较也跟着变成 `0.2.23`。

其余双方都改过的文件由 git 自动合并。交叉点这样处理：

- **流式首字超时换号**：`handleStreamingResponseWithReasoning` 保留上游的间隔计时器。每收到上游数据就更新最后读取时间；超时点若仍在间隔内就按剩余时间重新计时。OpenAI 在客户端还没看到模型输出时切换账号，`SafeToFailoverAfterWrite` 让这次切换计入首字切换预算，最多 1 次。TokenPro 的响应模型回写仍在同一函数里改 SSE 行，发生在判断“客户端是否已看到输出”之前，回写本身不算首字。生图组的 data-v1 / url-v1、native-v2、本机下载和签名下载路由没有改。
- **缓存写入计费**：`OpenAIUsage.billingUsageTokens` 带上 5 分钟 / 1 小时明细；总量缺失时用明细之和，避免缓存写入少计或按普通输入重复计。文本加图片仍是文本 token 一次，加上恰好一次 gpt-image-2 按张费用。缓存修复只作用在文本行，不把图片费再乘一次，也不把差额补进按张价格。
- **GPT-6.2 Sol / Astra**：加进 Codex 模型表、模型白名单和 OpenCode 预设。`tp-g` 前缀仍由入口中间件先还原成公开模型名，再进这张表。全局 Key 仍按还原后的模型选分组。
- **Ultra Fast 倍率**：支持列表加上 `gpt-6.2-sol` 和 `gpt-6.2-astra`。OAuth 账号先看套餐权益，再看账号目录里的 ultrafast 档。渠道 `ultrafast_multiplier` 只进入 token 档的 service tier 倍率。`calculateOpenAIImageCost` 不传 service tier，gpt-image-2 按张费用不乘这个倍率。6.2 在太平洋时间 2027-03-01 00:00 前默认 3 倍，之后 6 倍；GPT-6 Astra 和 GPT-6.1 Sol 仍是 6 倍。

没有重新生成 Ent 或 wire：上游没有改 schema。Go 工具链两边都是 1.27.4，`deploy/Dockerfile.lowmem` 不用改。

全局 Key、`tp-g` / `X-TokenPro-Group-Id`、生图组 data-v1 / url-v1、文本组图片白名单改写、签名下载，以及充值页「大额充值私聊站长 优惠」都保持原样。
