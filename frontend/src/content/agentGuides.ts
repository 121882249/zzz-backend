export type GuideKey =
  | "codex-desktop"
  | "claude-desktop";

export type GuideSection = {
  title: string;
  intro?: string;
  steps?: string[];
  fields?: string[][];
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
  sections: GuideSection[];
};

export const BRAND = "TokenPro";
export const API_BASE = "https://tokenpro.work";
export const OPENAI_BASE = API_BASE + "/v1";
export const API_KEY = "<你的 TokenPro API Key>";
export const GPT_MODEL = "<控制台中的 GPT 模型 ID>";
export const CLAUDE_MODEL = "<控制台中的 Claude 模型 ID>";

export const navItems: { key: GuideKey; label: string; meta: string }[] = [
  { key: "codex-desktop", label: "Codex 客户端", meta: "桌面端" },
  { key: "claude-desktop", label: "Claude 客户端", meta: "桌面端" },
];

const codexConfig = "model_provider = \"tokenpro\"\n"
  + "model = \"" + GPT_MODEL + "\"\n"
  + "model_reasoning_effort = \"high\"\n\n"
  + "[model_providers.tokenpro]\n"
  + "name = \"TokenPro\"\n"
  + "base_url = \"" + OPENAI_BASE + "\"\n"
  + "env_key = \"TOKENPRO_API_KEY\"\n"
  + "wire_api = \"responses\"";

export const guides: Record<GuideKey, Guide> = {
  "codex-desktop": {
    label: "Codex 客户端",
    eyebrow: "CODEX DESKTOP",
    title: "TokenPro 接入 Codex 客户端",
    description: "在 macOS 或 Windows 的 Codex 桌面版中配置 TokenPro Responses API。已安装 Codex 的用户可直接从“配置 TokenPro”开始。",
    tags: ["macOS", "Windows", "Responses API"],
    sections: [
      {
        title: "下载安装 Codex",
        intro: "只从 OpenAI 官方下载页或系统官方应用商店获取安装包，并核对发布者信息。",
        fields: [
          ["系统", "建议安装方式"],
          ["macOS Apple Silicon", "OpenAI 官方 Codex.dmg"],
          ["macOS Intel", "OpenAI 官方 x64 安装包"],
          ["Windows", "Microsoft Store 官方页面"],
          ["Linux", "请使用 Codex 命令行版本"],
        ],
        warning: "不要因为系统出现安全警告就直接忽略。先确认下载域名、代码签名和发布者均为官方来源。",
      },
      {
        title: "备份并配置 TokenPro",
        intro: "Codex Desktop 与 Codex CLI 共用 ~/.codex/config.toml。先备份，再把下面内容合并到现有配置。",
        code: [
          { label: "macOS / Linux · 备份", value: "mkdir -p ~/.codex\ncp ~/.codex/config.toml ~/.codex/config.toml.backup 2>/dev/null || true" },
          { label: "~/.codex/config.toml · 合并以下配置", value: codexConfig },
          { label: "Windows PowerShell · 备份", value: "New-Item -ItemType Directory -Force -Path \"$HOME\\.codex\" | Out-Null\nif (Test-Path \"$HOME\\.codex\\config.toml\") {\n  Copy-Item \"$HOME\\.codex\\config.toml\" \"$HOME\\.codex\\config.toml.backup\"\n}" },
        ],
        note: "不要用整段脚本覆盖配置文件，否则可能丢失已有 MCP、权限和模型设置。",
      },
      {
        title: "设置 API Key",
        code: [
          { label: "macOS · 当前终端", value: "export TOKENPRO_API_KEY=\"" + API_KEY + "\"" },
          { label: "Linux · 当前终端", value: "export TOKENPRO_API_KEY=\"" + API_KEY + "\"" },
          { label: "Windows PowerShell · 用户环境变量", value: "[Environment]::SetEnvironmentVariable(\n  \"TOKENPRO_API_KEY\",\n  \"" + API_KEY + "\",\n  \"User\"\n)" },
        ],
        note: "如需长期生效，请把变量保存到你实际使用的安全环境配置或密钥管理工具，而不是复制进项目仓库。",
      },
      {
        title: "重启、验证与恢复",
        steps: [
          "完全退出 Codex 客户端，再重新打开，确保新进程读取到环境变量。",
          "新建一个小型测试任务，在 TokenPro 控制台确认模型为 " + GPT_MODEL + "、协议为 Responses。",
          "出现 401 时，检查变量名是否与 config.toml 中 env_key 一致。",
          "需要切回原配置时，用 config.toml.backup 恢复并重新启动 Codex。",
        ],
      },
    ],
  },
  "claude-desktop": {
    label: "Claude 客户端",
    eyebrow: "CLAUDE DESKTOP",
    title: "TokenPro 接入 Claude 客户端",
    description: "Claude Desktop 本身不提供通用自定义供应商表单，可借助 CC Switch 管理 TokenPro 接入地址、密钥与恢复切换。",
    tags: ["macOS", "Windows", "CC Switch"],
    sections: [
      {
        title: "安装 Claude Desktop",
        steps: [
          "从 Claude 官方网站下载与你系统对应的桌面客户端。",
          "安装后先正常打开一次，确认应用可以运行。",
          "Windows 的 Claude Workspace 可能要求启用 Virtual Machine Platform。",
        ],
        warning: "下载与安装时核对官方域名和签名。第三方下载站提供的重新打包版本不建议使用。",
      },
      {
        title: "安装 CC Switch",
        intro: "建议使用当前稳定版。macOS 可使用 Homebrew，其他系统从 CC Switch 官方 Releases 下载。",
        code: [{ label: "macOS", value: "brew install --cask cc-switch" }],
        fields: [
          ["系统", "安装包"],
          ["macOS", ".dmg / .zip"],
          ["Windows", ".msi / Portable .zip"],
          ["Linux", ".deb / .rpm / .AppImage"],
        ],
      },
      {
        title: "添加 TokenPro 供应商",
        intro: "在 CC Switch 的 Claude Desktop 标签页新增自定义供应商，然后填写以下内容。",
        fields: [
          ["字段", "填写值"],
          ["供应商名称", BRAND],
          ["Anthropic Base URL", API_BASE],
          ["API Key", API_KEY],
          ["默认模型", CLAUDE_MODEL],
        ],
        steps: [
          "保存自定义供应商并在主页启用 TokenPro。",
          "如果 Claude Desktop 已打开，完全退出后重新启动。",
          "发送测试消息，并在 TokenPro 控制台检查 /v1/messages 请求。",
        ],
      },
      {
        title: "常见问题与恢复",
        steps: [
          "Windows 提示 Virtual Machine Platform unavailable：运行 optionalfeatures，启用“虚拟机平台”后重启电脑。",
          "切换无效：确认 CC Switch 当前启用的是 TokenPro，并完全重启 Claude Desktop。",
          "需要恢复官方订阅时，在 CC Switch 切回官方供应商或停用自定义配置。",
        ],
      },
    ],
  },
};
