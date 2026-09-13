export type GuideKey =
  | "codex-desktop"
  | "claude-desktop"
  | "codex-cli"
  | "claude-cli";

export type GuideSection = {
  title: string;
  intro?: string;
  steps?: string[];
  checklist?: string[];
  visual?: {
    title: string;
    caption: string;
    screen: "login" | "models" | "launch" | "verify" | "restore" | "bridge";
    callouts: string[];
  };
  fields?: string[][];
  links?: { label: string; href: string; meta: string }[];
  code?: { label: string; value: string }[];
  faq?: { question: string; answer: string }[];
  note?: string;
  warning?: string;
};

export type Guide = {
  label: string;
  eyebrow: string;
  title: string;
  description: string;
  tags: string[];
  summary: { label: string; value: string; code?: boolean }[];
  sections: GuideSection[];
};

export const TOKENPRO_VERSION = "v1.2.88+";

export const navItems: { key: GuideKey; label: string; meta: string }[] = [
  { key: "codex-desktop", label: "Codex 客户端", meta: "桌面端" },
  { key: "claude-desktop", label: "Claude 客户端", meta: "桌面端" },
  { key: "codex-cli", label: "Codex CLI", meta: "命令行" },
  { key: "claude-cli", label: "Claude Code", meta: "命令行" },
];

const sharedDownloads = [
  { label: "下载 TokenPro", href: "https://tokenpro.work/#download-dock-title", meta: "macOS · Windows · Linux" },
];

export const guides: Record<GuideKey, Guide> = {
  "codex-desktop": {
    label: "Codex 客户端",
    eyebrow: "CODEX DESKTOP",
    title: "用 TokenPro 打开 Codex 客户端",
    description: "无需手工修改配置文件，也无需复制 API Key。TokenPro 会读取你的可用分组、备份原配置、写入模型目录，并打开 Codex。",
    tags: ["自动配置", "多模型", "独立生图"],
    summary: [
      { label: "配置方式", value: "TokenPro 一键接入" },
      { label: "接口协议", value: "Responses API" },
      { label: "TokenPro 版本", value: TOKENPRO_VERSION, code: true },
    ],
    sections: [
      {
        title: "开始前检查",
        intro: "先安装 TokenPro 和 OpenAI 官方桌面客户端。若系统以 ChatGPT 桌面应用提供 Codex，TokenPro 也会自动识别。安装后请至少打开官方客户端一次。",
        links: [
          ...sharedDownloads,
          { label: "OpenAI 官方客户端", href: "https://developers.openai.com/codex/app", meta: "Codex / ChatGPT Desktop" },
        ],
        fields: [
          ["系统", "TokenPro 安装包"],
          ["macOS Apple 芯片", "macOS arm64 .dmg"],
          ["macOS Intel", "macOS x64 .dmg"],
          ["Windows", "Windows x64 .exe"],
          ["Linux", "Linux x64 .deb"],
        ],
        checklist: [
          "TokenPro 已安装，版本不低于 v1.2.45",
          "Codex / ChatGPT 官方桌面客户端已安装并至少启动过一次",
          "TokenPro 账户可以正常登录，且有可用订阅或余额",
          "当前系统用户对自己的配置目录有读写权限",
        ],
        warning: "只使用 TokenPro 官网和 OpenAI 官方入口下载。不要安装第三方重新打包的客户端。",
      },
      {
        title: "登录并更新 TokenPro",
        steps: [
          "打开 TokenPro，使用你的 TokenPro 邮箱和密码登录。",
          "查看右上角版本状态：显示“已是最新”时无需操作；显示黄色“发现更新”时点击即可在线更新。",
          "更新过程中等待进度条完成，TokenPro 会校验更新包并自动重启，不需要重新下载安装。",
          "点击钱包区域的“刷新”，确认余额与订阅信息已经同步。",
        ],
        visual: {
          title: "登录后的首页应该看到什么",
          caption: "先确认账户、版本和余额，再进入客户端配置。这样可避免把登录或额度问题误判为 Codex 配置失败。",
          screen: "login",
          callouts: ["账户状态", "版本更新", "余额刷新"],
        },
        note: "TokenPro 使用账户的全局 Key 自动接入 Codex，密钥保存在当前系统用户的私有配置目录中。",
      },
      {
        title: "选择模型并打开 Codex",
        steps: [
          "在 TokenPro 首页找到“Codex 客户端”，点击“模型选择”，再选择“选择模型”。",
          "模型按分组显示：订阅分组优先，余额分组按 GPT、Claude、Grok、Gemini 和其他厂商排列；同厂商模型保持在一起。",
          "首次使用或没有历史选择时，默认选中排序第一分组的第一个模型；你可以自由增加或取消其他模型。",
          "生图组独立显示在第一个余额 GPT 分组正下方，可不选、单选或多选；只选择生图模型时也可以直接生图。",
          "点击“应用并打开 Codex”。TokenPro 会自动备份并写入配置，然后启动或唤醒 Codex。",
        ],
        visual: {
          title: "模型选择界面图解",
          caption: "先选主对话模型，再按需选择独立生图模型。按钮显示已选数量，确认无误后再应用。",
          screen: "models",
          callouts: ["选择分组", "勾选模型", "应用并打开"],
        },
        note: "Codex 至少选择 1 个模型。OpenAI LLM 可以调用已选生图工具；Claude、Gemini、Grok 等非 OpenAI LLM 不会调用该工具，避免额外图片费用。独立生图模型不依赖 LLM。",
      },
      {
        title: "验证接入是否成功",
        steps: [
          "Codex 打开后，新建一个临时对话，确认模型选择器里出现刚才勾选的模型。",
          "发送“只回复 OK”作为最小测试；能正常返回即可确认登录、路由和模型三部分都已连通。",
          "如果选择了生图模型，再单独发送一个简单生图请求；文字模型和生图模型应分别正常工作。",
          "在 TokenPro 账户记录或用量页面确认出现本次测试请求，核对模型名称与扣费分组。",
        ],
        visual: {
          title: "一分钟验收",
          caption: "看到模型、收到回复、查到用量，三项全部通过才算接入完成。",
          screen: "verify",
          callouts: ["模型已出现", "请求有响应", "用量可追踪"],
        },
        note: "建议先用最短文本请求验证，再开始长任务，避免配置异常时产生不必要的等待。",
      },
      {
        title: "切换模型与恢复官方配置",
        steps: [
          "需要增加、减少或更换模型时，回到 TokenPro 的“模型选择”重新勾选并应用。",
          "点击“打开应用”只会唤醒正在运行的 Codex，不会为了切换窗口而强制终止当前任务。",
          "需要回到 OpenAI 官方配置时，打开“模型选择”，点击“恢复官方配置”。TokenPro 会恢复接入前保存的配置并重新打开 Codex。",
          "遇到 401 或模型列表未刷新时，先在 TokenPro 退出后重新登录，再重新应用模型。",
        ],
        visual: {
          title: "恢复操作不会丢失原配置",
          caption: "TokenPro 接入前会保存官方配置。点击恢复后，将备份写回并重新打开 Codex。",
          screen: "restore",
          callouts: ["打开模型选择", "恢复官方配置", "重新打开 Codex"],
        },
        warning: "不要在 TokenPro 已接入期间手工覆盖 ~/.codex/config.toml；这可能删除原有 MCP、权限或项目设置。",
      },
      {
        title: "常见问题",
        faq: [
          { question: "点击后没有打开 Codex？", answer: "先手工启动一次官方客户端并完成系统权限确认，然后完全退出客户端，再回到 TokenPro 点击“应用并打开 Codex”。" },
          { question: "模型列表还是旧的？", answer: "退出并重新登录 TokenPro，点击余额刷新，再重新进入模型选择并应用。仍未更新时，完整退出 Codex 后重试。" },
          { question: "出现 401 或无权限提示？", answer: "通常是本地凭据过期或账户状态未同步。重新登录 TokenPro、确认余额或订阅可用，然后重新应用配置。" },
          { question: "原来的 MCP 和项目设置还在吗？", answer: "正常情况下会保留。若你在接入期间手工覆盖 config.toml，可能破坏合并结果；此时先恢复官方配置，再重新接入。" },
        ],
      },
    ],
  },
  "claude-desktop": {
    label: "Claude 客户端",
    eyebrow: "CLAUDE DESKTOP",
    title: "用 TokenPro 打开 Claude 客户端",
    description: "最新版 TokenPro 已内置 Claude 本地安全桥接，不再需要 CC Switch。选择模型后，TokenPro 会配置独立的第三方账户并打开 Claude。",
    tags: ["无需 CC Switch", "本地桥接", "自动路由"],
    summary: [
      { label: "配置方式", value: "TokenPro 本地安全桥接" },
      { label: "本地监听", value: "127.0.0.1:23179", code: true },
      { label: "TokenPro 版本", value: TOKENPRO_VERSION, code: true },
    ],
    sections: [
      {
        title: "开始前检查",
        intro: "先安装 TokenPro 和 Anthropic 官方 Claude Desktop，并各自打开一次。Claude Desktop 当前支持 macOS、Windows 和 Linux；请按官方页面选择与你系统匹配的版本。",
        links: [
          ...sharedDownloads,
          { label: "Claude 官方下载", href: "https://claude.com/download", meta: "macOS · Windows · Linux" },
          { label: "Claude 官方安装说明", href: "https://support.claude.com/en/articles/10065433-install-claude-desktop", meta: "系统要求与安装步骤" },
        ],
        checklist: [
          "TokenPro 已安装，版本不低于 v1.2.45",
          "Claude Desktop 已安装并至少启动过一次",
          "TokenPro 账户有可用订阅或余额",
          "本机 127.0.0.1:23179 端口未被其他程序占用",
        ],
        warning: "不再安装或配置 CC Switch。旧教程中的 Anthropic Base URL、API Key 和默认模型表单已不适用于当前 TokenPro 客户端。",
      },
      {
        title: "登录并更新 TokenPro",
        steps: [
          "打开 TokenPro，使用你的 TokenPro 邮箱和密码登录。",
          "确认右上角显示“已是最新”；若显示黄色更新提示，点击后等待进度完成并让程序自动重启。",
          "点击钱包区域的“刷新”，确认余额、订阅数量、订阅余额和到期时间已经同步。",
        ],
        visual: {
          title: "登录后的首页应该看到什么",
          caption: "账户、版本、余额三项正常后再配置 Claude，可明显减少排查范围。",
          screen: "login",
          callouts: ["账户状态", "版本更新", "余额刷新"],
        },
        note: "Claude 只会获得随机生成的本机桥接凭据，不会直接读取你的 TokenPro 全局 Key。桥接只监听 127.0.0.1，不向局域网开放。",
      },
      {
        title: "选择模型并打开 Claude",
        steps: [
          "在 TokenPro 首页找到“Claude 客户端”，点击“模型选择”，再选择“选择模型”。",
          "Claude 的分组顺序为：订阅分组、Claude、GPT、Grok、Gemini、其他；同厂商模型集中排列，不会散落。",
          "首次使用或没有历史选择时，默认选中排序第一分组的第一个模型；至少选择 1 个 LLM，也可以同时选择多个。",
          "Claude 客户端不会显示生图模型；这些模型只在 Codex 的独立生图组中提供。",
          "点击“应用并打开 Claude”。TokenPro 会启动本地桥接、写入独立的第三方配置，并打开 Claude。",
        ],
        visual: {
          title: "选择模型并启动本地桥接",
          caption: "Claude 只展示对话模型。应用时 TokenPro 会同时准备本地安全桥接和独立的第三方账户配置。",
          screen: "bridge",
          callouts: ["选择对话模型", "启动本地桥接", "打开 Claude"],
        },
        note: "多个模型共用一把 TokenPro 全局 Key；本地桥接会根据当前模型自动选择对应分组，无需手工切换 Key。",
      },
      {
        title: "验证连接",
        steps: [
          "Claude 打开后，账户位置应显示你的 TokenPro 用户账户；新建对话并发送一个简短测试请求。",
          "确认回复正常后，到 TokenPro 用量记录中核对本次请求的模型与分组。",
          "如果仍出现 Sign In 页面，请完全退出 Claude，再从 TokenPro 点击“打开应用”；必要时重新选择模型并应用。",
          "如果提示本地桥接无法启动，先退出所有 Claude 窗口，确认 23179 端口未被其他程序占用，再重试。",
          "退出 TokenPro 打开的 Claude 后，本地桥接会停止并取消仍在进行的上游请求，避免遗留请求持续消耗。",
        ],
        visual: {
          title: "本地桥接连接关系",
          caption: "Claude 只连接本机回环地址；TokenPro 再使用你的账户权限访问服务端，局域网中的其他设备无法访问该端口。",
          screen: "verify",
          callouts: ["Claude 已登录", "桥接仅限本机", "用量可追踪"],
        },
        warning: "不要直接从系统启动器复制或修改 TokenPro 的 Claude-3p 配置目录；始终通过 TokenPro 选择模型、打开应用或恢复官方配置。",
      },
      {
        title: "恢复官方账户",
        steps: [
          "完全结束正在运行的 Claude 对话，避免恢复时中断重要任务。",
          "回到 TokenPro 首页，在“Claude 客户端”卡片中打开“模型选择”。",
          "点击“恢复官方配置”，等待 TokenPro 写回接入前保存的配置。",
          "按提示重新打开 Claude，使用原 Anthropic 官方账户继续登录。",
        ],
        visual: {
          title: "恢复流程",
          caption: "恢复只影响 TokenPro 创建的第三方接入配置，不会删除 Claude 的普通聊天记录。",
          screen: "restore",
          callouts: ["结束当前任务", "恢复官方配置", "重新打开 Claude"],
        },
      },
      {
        title: "常见问题",
        faq: [
          { question: "Claude 一直停在 Sign In？", answer: "完全退出 Claude（包括托盘或菜单栏进程），确认 TokenPro 已登录，然后从 TokenPro 重新应用模型并打开。" },
          { question: "提示 23179 端口被占用？", answer: "退出 TokenPro 和 Claude 后重试。若仍占用，检查是否有旧版 TokenPro、代理工具或其他本地服务监听该端口。" },
          { question: "为什么 Claude 中没有生图模型？", answer: "这是预期行为。Claude 客户端只提供对话模型，独立生图模型在 Codex 客户端的模型选择中使用。" },
          { question: "关闭 Claude 后请求还会继续吗？", answer: "通过 TokenPro 启动的 Claude 退出后，本地桥接会停止，并取消仍在进行的上游请求。" },
        ],
      },
    ],
  },
  "codex-cli": {
    label: "Codex CLI",
    eyebrow: "CODEX COMMAND LINE",
    title: "用 TokenPro 连接 Codex CLI",
    description: "TokenPro 为 Codex CLI 创建完全隔离的配置目录和启动入口，不修改系统 PATH，也不会覆盖你在普通终端中使用的官方 Codex 配置。",
    tags: ["配置隔离", "模型多选", "支持生图"],
    summary: [
      { label: "配置目录", value: "TokenPro/cli/codex" },
      { label: "图片桥接", value: "127.0.0.1:23182", code: true },
      { label: "TokenPro 版本", value: TOKENPRO_VERSION, code: true },
    ],
    sections: [
      {
        title: "开始前检查",
        intro: "先安装 TokenPro 与 OpenAI Codex CLI。TokenPro 只负责生成独立配置和启动已安装的命令行工具，不会替代 Codex CLI 本身。",
        checklist: [
          "TokenPro 已更新到 v1.2.88 或更高版本",
          "在普通终端运行 codex --version 能显示版本号",
          "TokenPro 账户有可用订阅或余额",
          "准备新建一个临时目录用于首次连接测试",
        ],
        note: "从 TokenPro 启动时，CODEX_HOME 只对新打开的终端生效；系统原来的 codex 命令仍使用官方配置。",
      },
      {
        title: "选择模型并连接命令行",
        steps: [
          "打开 TokenPro 首页，在“Codex 命令行”卡片点击“模型选择”。",
          "至少选择 1 个对话模型；如需图片能力，可同时选择独立生图模型。",
          "点击应用保存选择，卡片会显示“已选 N 个模型”。",
          "点击“连接命令行”。TokenPro 会打开一个已注入独立 CODEX_HOME 的新终端并启动 Codex。",
          "以后再次连接会沿用保存的模型；需要变更时重新进入模型选择即可。",
        ],
        visual: {
          title: "CLI 配置与官方配置相互隔离",
          caption: "TokenPro 只向新终端注入独立配置目录，不修改全局环境变量和系统 PATH。",
          screen: "launch",
          callouts: ["选择模型", "独立 CODEX_HOME", "连接命令行"],
        },
        warning: "不要把 TokenPro 终端里的 CODEX_HOME 手工复制成系统全局环境变量，否则会失去配置隔离。",
      },
      {
        title: "验证与日常使用",
        steps: [
          "在 TokenPro 打开的终端中新建一个对话，发送“只回复 OK”进行最小测试。",
          "在 Codex 的模型列表中确认已选择的模型可见，并尝试切换一次。",
          "回到 TokenPro 用量记录，核对请求模型和计费分组。",
          "普通终端直接运行 codex 可继续使用官方配置；需要 TokenPro 配置时，从客户端连接或运行数据目录中的 bin/tokenpro-codex。",
        ],
        visual: {
          title: "一分钟验收",
          caption: "模型可见、请求成功、用量可查，三项均正常即可开始正式任务。",
          screen: "verify",
          callouts: ["模型已同步", "请求成功", "用量正确"],
        },
      },
      {
        title: "常见问题",
        faq: [
          { question: "点击连接后没有打开终端？", answer: "先确认系统默认终端可以正常启动，并在普通终端运行 codex --version。修复 CLI 安装后回到 TokenPro 重试。" },
          { question: "为什么普通终端看不到 TokenPro 模型？", answer: "这是配置隔离的预期结果。请从 TokenPro 点击“连接命令行”，或运行 TokenPro 数据目录中的 tokenpro-codex 启动入口。" },
          { question: "重新选择模型会终止其他应用吗？", answer: "不会。桌面端与命令行配置相互独立，Codex 与 Claude 也各自独立。" },
        ],
      },
    ],
  },
  "claude-cli": {
    label: "Claude Code",
    eyebrow: "CLAUDE CODE",
    title: "用 TokenPro 连接 Claude Code",
    description: "TokenPro 通过独立 CLAUDE_CONFIG_DIR 与本地安全桥接接入 Claude Code，模型列表、凭据和官方配置互不覆盖。",
    tags: ["本地桥接", "配置隔离", "模型列表同步"],
    summary: [
      { label: "最低版本", value: "Claude Code 2.1.242+", code: true },
      { label: "本地监听", value: "127.0.0.1:23181", code: true },
      { label: "TokenPro 版本", value: TOKENPRO_VERSION, code: true },
    ],
    sections: [
      {
        title: "开始前检查",
        intro: "Claude Code 需要 2.1.242 或更高版本，才能读取 TokenPro 生成的 modelPicker 模型列表。Windows 必须安装原生命令行版本，WSL 需要单独配置。",
        checklist: [
          "TokenPro 已更新到 v1.2.88 或更高版本",
          "claude --version 显示 2.1.242 或更高版本",
          "TokenPro 账户有可用订阅或余额",
          "本机 127.0.0.1:23181 端口可用",
        ],
        warning: "Windows 的 WSL 与 Windows 原生环境相互隔离；当前一键连接面向原生 Claude Code，不会自动配置 WSL。",
      },
      {
        title: "选择模型并连接",
        steps: [
          "在 TokenPro 首页找到“Claude 命令行”，点击“模型选择”。",
          "选择至少 1 个对话模型。Claude Code 不显示独立生图模型。",
          "点击应用，确认卡片出现“已选 N 个模型”状态。",
          "点击“连接命令行”。TokenPro 会启动 23181 本地桥接，并打开带独立 CLAUDE_CONFIG_DIR 的新终端。",
          "进入 Claude Code 后使用 /model 查看并切换已选模型。",
        ],
        visual: {
          title: "Claude Code 连接关系",
          caption: "Claude Code 只读取 TokenPro 的独立设置并连接本机桥接；全局 settings.json 不会被修改。",
          screen: "bridge",
          callouts: ["选择模型", "启动 23181 桥接", "使用 /model 切换"],
        },
        note: "再次连接会沿用上次选择。桌面端和命令行使用不同端口、令牌与配置目录，互不干扰。",
      },
      {
        title: "验证连接",
        steps: [
          "在新终端中执行 /model，确认列表与 TokenPro 中的选择一致。",
          "发送一个最短测试请求并等待完整回复，确认没有空响应或提前断流。",
          "回到 TokenPro 用量记录，核对模型名称与计费分组。",
          "直接在普通终端运行 claude 仍会使用官方配置；需要 TokenPro 时，从客户端连接或运行 bin/tokenpro-claude。",
        ],
        visual: {
          title: "一分钟验收",
          caption: "模型列表、完整回复和用量记录全部正常，说明 CLI、桥接与服务端路由均已连接。",
          screen: "verify",
          callouts: ["/model 正确", "回复完整", "用量可查"],
        },
      },
      {
        title: "常见问题",
        faq: [
          { question: "/model 没有显示所选模型？", answer: "先升级 Claude Code 至 2.1.242 或更高版本，再回到 TokenPro 重新应用模型并连接命令行。" },
          { question: "提示本地桥接无法启动？", answer: "退出旧版 TokenPro 和已连接的 Claude Code 终端后重试，并检查 23181 端口是否被其他程序占用。" },
          { question: "会修改全局 Claude 配置吗？", answer: "不会。TokenPro 使用独立 CLAUDE_CONFIG_DIR 和 --settings 文件，普通终端中的官方 Claude 配置保持不变。" },
        ],
      },
    ],
  },
};
