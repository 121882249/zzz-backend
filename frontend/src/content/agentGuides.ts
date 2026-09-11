export type GuideKey =
  | "codex-desktop"
  | "claude-desktop";

export type GuideSection = {
  title: string;
  intro?: string;
  steps?: string[];
  fields?: string[][];
  links?: { label: string; href: string; meta: string }[];
  code?: { label: string; value: string }[];
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

export const TOKENPRO_VERSION = "v1.2.45+";

export const navItems: { key: GuideKey; label: string; meta: string }[] = [
  { key: "codex-desktop", label: "Codex 客户端", meta: "桌面端" },
  { key: "claude-desktop", label: "Claude 客户端", meta: "桌面端" },
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
        title: "准备两个客户端",
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
        note: "Codex 至少选择 1 个模型。OpenAI LLM 可以调用已选生图工具；Claude、Gemini、Grok 等非 OpenAI LLM 不会调用该工具，避免额外图片费用。独立生图模型不依赖 LLM。",
      },
      {
        title: "日常切换与恢复官方配置",
        steps: [
          "需要增加、减少或更换模型时，回到 TokenPro 的“模型选择”重新勾选并应用。",
          "点击“打开应用”只会唤醒正在运行的 Codex，不会为了切换窗口而强制终止当前任务。",
          "需要回到 OpenAI 官方配置时，打开“模型选择”，点击“恢复官方配置”。TokenPro 会恢复接入前保存的配置并重新打开 Codex。",
          "遇到 401 或模型列表未刷新时，先在 TokenPro 退出后重新登录，再重新应用模型。",
        ],
        warning: "不要在 TokenPro 已接入期间手工覆盖 ~/.codex/config.toml；这可能删除原有 MCP、权限或项目设置。",
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
        title: "准备两个客户端",
        intro: "先安装 TokenPro 和 Anthropic 官方 Claude Desktop，并各自打开一次。Claude Desktop 当前支持 macOS、Windows 和 Linux；请按官方页面选择与你系统匹配的版本。",
        links: [
          ...sharedDownloads,
          { label: "Claude 官方下载", href: "https://claude.com/download", meta: "macOS · Windows · Linux" },
          { label: "Claude 官方安装说明", href: "https://support.claude.com/en/articles/10065433-install-claude-desktop", meta: "系统要求与安装步骤" },
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
        note: "多个模型共用一把 TokenPro 全局 Key；本地桥接会根据当前模型自动选择对应分组，无需手工切换 Key。",
      },
      {
        title: "检查连接与恢复官方配置",
        steps: [
          "Claude 打开后，账户位置应显示你的 TokenPro 用户账户；新建对话并发送一个简短测试请求。",
          "如果仍出现 Sign In 页面，请完全退出 Claude，再从 TokenPro 点击“打开应用”；必要时重新选择模型并应用。",
          "如果提示本地桥接无法启动，先退出所有 Claude 窗口，确认 23179 端口未被其他程序占用，再重试。",
          "退出 TokenPro 打开的 Claude 后，本地桥接会停止并取消仍在进行的上游请求，避免遗留请求持续消耗。",
          "需要恢复 Anthropic 官方账户时，打开“模型选择”，点击“恢复官方配置”。",
        ],
        warning: "不要直接从系统启动器复制或修改 TokenPro 的 Claude-3p 配置目录；始终通过 TokenPro 选择模型、打开应用或恢复官方配置。",
      },
    ],
  },
};
