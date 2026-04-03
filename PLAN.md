# Crush — 补齐 Claude Code 功能计划

> 对比 `~/claude-code` 源码整理，按优先级分 10 个阶段。

---

## 现状对比总览

| 维度 | Crush | Claude Code | 差距 |
|---|---|---|---|
| 工具数 | ~28 | ~40+ | 中 |
| 斜杠命令 | ~18 (命令面板) | ~60+ (文本输入) | 大 |
| 权限系统 | yolo/允许列表/工作目录 | 三态规则/多层源/分类器/持久化 | 大 |
| 记忆系统 | memory_search (只读) | memdir + 自动提取 + 召回 + dream | 大 |
| Hooks | 无 (REVIEW.md 中有 TODO) | 26 事件 / 5 种 hook 类型 | 大 |
| Plugin | 无 | 完整 marketplace + DXT | 大 |
| CLAUDE.md | 平面扫描当前目录 | 向上遍历 + @include + 条件规则 | 中 |
| UI | Bubble Tea TUI | React/Ink + 主题 + vim | 中 |
| 远程/IDE/语音 | 无 | bridge/IDE/voice | 大 (长期) |

---

## 阶段 1: 权限系统增强 (基础设施)

> 依赖: 无。被阶段 2 (hooks)、阶段 6 (auto mode) 依赖。

### 1.1 三态权限规则 (allow/deny/ask)

**新建 `internal/permission/rule.go`**:

```go
type RuleBehavior string // "allow" | "deny" | "ask"
type RuleSource  string // "userSettings" | "projectSettings" | "localSettings" | "cliArg" | "session"

type PermissionRule struct {
    Source      RuleSource
    Behavior    RuleBehavior
    ToolName    string
    RuleContent string // 可选，如 "npm test"
}
```

- `ParseRule(s string) (toolName, ruleContent)` — 解析 `Bash(npm test:*)` 语法
  - 找第一个未转义 `(` 和最后一个未转义 `)`
  - 提取 toolName 和 ruleContent
  - 反转义 `\(` → `(`, `\)` → `)`, `\\` → `\`
  - `Bash()` 和 `Bash(*)` 视为全工具规则 (无 content)

**新建 `internal/permission/rule_match.go`**:

三种匹配类型:
```go
type ShellRuleType string // "exact" | "prefix" | "wildcard"

type ShellPermissionRule struct {
    Type    ShellRuleType
    Pattern string
}
```

- 解析逻辑:
  - 以 `:*` 结尾 → `prefix` (去掉 `:*`，前缀匹配)
  - 含未转义 `*` (非 `:*`) → `wildcard`
  - 否则 → `exact`
- 通配符匹配 `matchWildcardPattern`:
  1. `\*` → 字面星号占位符, `\\` → 字面反斜杠占位符
  2. 转义所有 regex 特殊字符 (除 `*`)
  3. `*` → `.*`
  4. 恢复占位符
  5. 特殊: 模式以 ` *` 结尾且只有一个 `*` 时，末尾空格+参数变为可选 (如 `git *` 匹配 `git` 和 `git add`)
  6. 使用 `(?s)` (dotAll) 使通配符匹配换行符

**修改 `internal/permission/permission.go`**:

- `Service` 接口增加:
  ```go
  AddRule(rule PermissionRule)
  RemoveRule(rule PermissionRule)
  Rules() []PermissionRule
  ```
- 请求流程改为:
  1. deny rule (全工具) → DENY
  2. ask rule (全工具) → ASK
  3. tool-specific check (现有逻辑)
  4. content-specific deny/ask rule → 对应行为
  5. bypass 模式 (除安全检查) → ALLOW
  6. allow rule (全工具) → ALLOW
  7. content-specific allow rule → ALLOW
  8. passthrough → ASK (提示用户)

### 1.2 多层规则源

**新建 `internal/permission/loader.go`**:

```go
func LoadRules(configStore *config.ConfigStore) []PermissionRule
```

- 从多个 settings JSON 文件加载:
  - `~/.config/crush/settings.json` (userSettings)
  - `.crush/settings.json` (projectSettings)
  - `.crush/settings.local.json` (localSettings)
- JSON 格式:
  ```json
  {
    "permissions": {
      "allow": ["Bash(npm:*)", "Read"],
      "deny": ["Bash(rm -rf:*)"],
      "ask": ["Bash(git push:*)"]
    }
  }
  ```
- 合并优先级: localSettings > projectSettings > userSettings

**修改 `internal/config/config.go`**:

```go
type Permissions struct {
    AllowedTools         []string `json:"allowed_tools,omitempty"`
    SkipRequests         bool     `json:"skip_requests,omitempty"`
    AutoApproveWorkingDir bool    `json:"auto_approve_working_dir,omitempty"`
    // 新增:
    AllowRules []string `json:"allow,omitempty"`
    DenyRules  []string `json:"deny,omitempty"`
    AskRules   []string `json:"ask,omitempty"`
}
```

### 1.3 规则持久化

**修改 `internal/config/store.go`**:

```go
func (s *ConfigStore) AddPermissionRule(scope Scope, behavior string, rule string) error
func (s *ConfigStore) RemovePermissionRule(scope Scope, behavior string, rule string) error
```

- 用 `sjson` 追加/删除 JSON 数组元素
- `GrantPersistent` 调用 `AddPermissionRule(ScopeWorkspace, "allow", rule)`

### 1.4 危险模式检测

**新建 `internal/permission/dangerous.go`**:

```go
var dangerousBashPrefixes = []string{
    "python", "python3", "python2", "node", "deno", "tsx",
    "ruby", "perl", "php", "lua", "npx", "bunx",
    "npm run", "yarn run", "pnpm run", "bun run",
    "bash", "sh", "zsh", "fish", "eval", "exec",
    "env", "xargs", "sudo", "ssh",
}

func IsDangerousBashPermission(ruleContent string) bool
```

**新建 `internal/permission/filesystem.go`**:

```go
var protectedPaths = []string{
    ".git/", ".gitconfig", ".bashrc", ".zshrc", ".profile",
    ".ssh/", ".gnupg/", ".crush/", ".claude/",
}

func IsProtectedPath(path string) bool
```

- 即使 bypass 模式也对这些路径要求确认

### 1.5 权限模式

**新建 `internal/permission/mode.go`**:

```go
type Mode int
const (
    ModeDefault Mode = iota
    ModePlan              // 只读
    ModeAcceptEdits       // 自动批准文件编辑, bash 仍需确认
    ModeBypassPermissions // 全部批准 (除 IsProtectedPath)
)
```

- 请求流程中加入模式判断
- Shift+Tab 循环: Default → AcceptEdits → Plan → BypassPermissions → Default
- UI 修改 `internal/ui/model/ui.go`: 监听 Shift+Tab 按键

---

## 阶段 2: Hooks 系统

> 依赖: 阶段 1 (权限规则解析器复用)

### 2.1 事件与类型

**新建 `internal/hooks/event.go`**:

```go
type Event string
const (
    PreToolUse       Event = "PreToolUse"
    PostToolUse      Event = "PostToolUse"
    SessionStart     Event = "SessionStart"
    SessionEnd       Event = "SessionEnd"
    UserPromptSubmit Event = "UserPromptSubmit"
    PreCompact       Event = "PreCompact"
    PostCompact      Event = "PostCompact"
    Stop             Event = "Stop"
    Notification     Event = "Notification"
    ConfigChange     Event = "ConfigChange"
)
```

**新建 `internal/hooks/types.go`**:

```go
type HookMatcher struct {
    Matcher string     `json:"matcher,omitempty"` // 过滤字段 (如 tool name)
    Hooks   []HookSpec `json:"hooks"`
}

type HookSpec struct {
    Type          string            `json:"type"` // "command" | "http" | "prompt"
    Command       string            `json:"command,omitempty"`
    URL           string            `json:"url,omitempty"`
    Prompt        string            `json:"prompt,omitempty"`
    If            string            `json:"if,omitempty"` // 权限规则语法过滤
    Timeout       int               `json:"timeout,omitempty"` // 秒
    StatusMessage string            `json:"statusMessage,omitempty"`
    Once          bool              `json:"once,omitempty"`
    Async         bool              `json:"async,omitempty"`
    Shell         string            `json:"shell,omitempty"` // "bash" (默认)
    Headers       map[string]string `json:"headers,omitempty"` // HTTP hook
    Model         string            `json:"model,omitempty"`   // prompt hook
}

type HookResult struct {
    Continue      bool   `json:"continue"`
    Decision      string `json:"decision,omitempty"` // "approve" | "block"
    Reason        string `json:"reason,omitempty"`
    SystemMessage string `json:"systemMessage,omitempty"`
    SuppressOutput bool  `json:"suppressOutput,omitempty"`
    // updatedInput 用于 PreToolUse 修改 tool input
    UpdatedInput  map[string]any `json:"updatedInput,omitempty"`
}

type HookInput struct {
    Event     Event          `json:"event"`
    ToolName  string         `json:"toolName,omitempty"`
    ToolInput map[string]any `json:"toolInput,omitempty"`
    SessionID string         `json:"sessionId,omitempty"`
    ProjectRoot string       `json:"projectRoot,omitempty"`
    CWD       string         `json:"cwd,omitempty"`
}
```

### 2.2 配置加载

**新建 `internal/hooks/config.go`**:

```go
type Settings map[Event][]HookMatcher

func LoadHooksFromConfig(cfg *config.Config) Settings
```

**修改 `internal/config/config.go`**:

```go
type Config struct {
    // ... 现有字段
    Hooks map[string][]json.RawMessage `json:"hooks,omitempty"`
}
```

**新建 `internal/hooks/snapshot.go`**:

```go
type Snapshot struct {
    settings Settings
    mu       sync.RWMutex
}

func CaptureSnapshot(settings Settings) *Snapshot
func (s *Snapshot) Get() Settings
```

### 2.3 执行引擎

**新建 `internal/hooks/executor.go`**:

```go
func ExecuteHooks(ctx context.Context, snapshot *Snapshot, input HookInput) ([]HookResult, error)
```

- **Command hook 执行**:
  1. 序列化 `HookInput` 为 JSON
  2. 通过 stdin 管道传给 shell 命令
  3. 设置环境变量: `CRUSH_SESSION_ID`, `CRUSH_PROJECT_ROOT`, `CRUSH_CWD`
  4. 解析 stdout 为 `HookResult` JSON
  5. Exit code 语义: 0=成功, 2=阻止 (stderr 作为原因), 其他=非阻止错误
  6. 超时通过 `context.WithTimeout` 控制

- **HTTP hook 执行**:
  1. POST `HookInput` JSON 到 URL
  2. SSRF 防护: 解析 URL host, DNS 查询, 拒绝私有/链路本地 IP
  3. 解析响应为 `HookResult`

- **Prompt hook 执行**:
  1. 调用 small model, system prompt 强制 JSON 输出
  2. 返回 `{ok: bool, reason: string}`
  3. 不 ok → 阻止

**新建 `internal/hooks/match.go`**:

```go
func GetMatchingHooks(snapshot *Snapshot, input HookInput) []HookSpec
```

- 遍历事件的 matcher 列表
- 检查 `matcher` 字符串是否匹配 (如 tool name)
  - 支持 `|` 分隔的多值: `Write|Edit`
- 检查 `if` 条件 (复用 `permission.ParseRule`)

### 2.4 Agent 集成点

**修改 `internal/agent/coordinator.go`**:

tool 执行包装:
```go
// Before tool execution:
results := hooks.ExecuteHooks(ctx, snapshot, HookInput{Event: PreToolUse, ToolName: name, ToolInput: input})
for _, r := range results {
    if r.Decision == "block" { return blocked error }
    if r.UpdatedInput != nil { merge into input }
}
// Execute tool...
// After tool execution:
hooks.ExecuteHooks(ctx, snapshot, HookInput{Event: PostToolUse, ToolName: name, ...})
```

**修改 `internal/agent/session_agent.go`**:

- session 创建时: `ExecuteHooks(SessionStart)`
- session 结束时: `ExecuteHooks(SessionEnd)` (1.5s 超时)
- 用户提交消息时: `ExecuteHooks(UserPromptSubmit)`

---

## 阶段 3: CLAUDE.md 增强 & 记忆系统

> 依赖: 无 (可与阶段 1-2 并行)

### 3.1 目录向上遍历

**新建 `internal/config/context.go`** (或提取自 prompt.go):

```go
func DiscoverContextFiles(cwd string) ([]ContextFile, error)
```

算法:
1. 从 CWD 向上遍历到文件系统根
2. 收集所有目录到列表, 然后**反转** (根→CWD, 越近优先级越高)
3. 每层检查:
   - `<dir>/CLAUDE.md`
   - `<dir>/.claude/CLAUDE.md`
   - `<dir>/.claude/rules/*.md` (递归子目录)
   - `<dir>/CLAUDE.local.md`
4. 全局层 (最低优先级):
   - `~/.crush/CLAUDE.md`
   - `~/.crush/rules/*.md`
   - `~/.config/crush/CLAUDE.md`
5. 保留现有兼容名: `AGENTS.md`, `crush.md`, `CRUSH.md` 等
6. 去重 (按小写 basename)

### 3.2 @include 指令

**新建 `internal/config/include.go`**:

```go
func ProcessIncludes(content string, basePath string, depth int, seen map[string]bool) (string, []ContextFile, error)
```

- 正则: `(?:^|\s)@((?:[^\s\\]|\\ )+)` — 匹配 `@path` 语法
- 路径解析:
  - `@./path` → 相对于当前文件所在目录
  - `@~/path` → home 目录展开
  - `@/path` → 绝对路径
  - `@path` → 相对路径
- 跳过 code block 和 code span 中的 @引用 (用简单的 ``` 状态机)
- 递归深度限制: `MAX_INCLUDE_DEPTH = 5`
- 循环引用检测: 通过 `seen` set (规范化路径)
- 只允许文本文件扩展名

### 3.3 条件规则 (paths: frontmatter)

**新建 `internal/config/frontmatter.go`**:

```go
type Frontmatter struct {
    Paths       []string `yaml:"paths,omitempty"`
    Description string   `yaml:"description,omitempty"`
}

func ParseFrontmatter(content string) (Frontmatter, string, error)
```

- 正则: `^---\s*\n([\s\S]*?)---\s*\n?`
- paths 支持逗号分隔字符串或 YAML 数组
- 花括号展开: `src/*.{ts,tsx}` → `["src/*.ts", "src/*.tsx"]`

**条件规则匹配**:
- 有 `paths` 的规则不在启动时加载
- 文件操作时 (edit/write/view), 检查操作文件路径是否匹配任一条件规则的 glob
- 匹配的规则动态注入为 system message

### 3.4 自动记忆系统 (memdir)

**新建 `internal/memdir/` 包**:

**`types.go`**:
```go
type MemoryType string // "user" | "feedback" | "project" | "reference"

type MemoryHeader struct {
    Name        string     `yaml:"name"`
    Description string     `yaml:"description"`
    Type        MemoryType `yaml:"type"`
    Path        string     // 文件路径
    ModTime     time.Time
}
```

4 种记忆类型含义:
- `user`: 角色、目标、偏好
- `feedback`: 纠正和确认
- `project`: 不可推导的上下文 (截止日期、事件、决策)
- `reference`: 外部系统指针 (Linear, Grafana, Slack)

明确排除: 代码模式、架构、git 历史、文件结构、调试方案

**`memdir.go`**:
```go
const (
    MaxEntrypointLines = 200
    MaxEntrypointBytes = 25 * 1024
)

func MemoryDir(cwd string) string // ~/.crush/projects/<slug>/memory/
func EnsureDir(dir string) error
func TruncateEntrypoint(content string) string
```

- 路径: `~/.crush/projects/<sanitized-git-root>/memory/`
- git worktree 共享同一 memory 目录 (通过 `findCanonicalGitRoot`)
- `MEMORY.md` 格式: 每行 `- [Title](file.md) — 一行描述`

**`scan.go`**:
```go
func ScanMemoryFiles(dir string) ([]MemoryHeader, error)
```

- `readdir` 递归扫描 `.md` 文件 (排除 MEMORY.md)
- 每个文件读取前 30 行, 解析 frontmatter
- 按 mtime 降序排列, 最多 200 个

**`extract.go`**:
```go
func ExtractMemories(ctx context.Context, agent AgentFactory, memDir string, messages []Message) error
```

- 触发: 每次完整对话循环结束后 (fire-and-forget goroutine)
- 互斥: 如果主 agent 已写入 memory 目录则跳过
- Fork 子 agent:
  - 工具: FileRead, Grep, Glob, 只读 Bash, Edit/Write (仅限 memory 目录)
  - 最多 5 步
  - Prompt: "turn 1 — 并行读取; turn 2 — 并行写入"
- 提前注入: 扫描 memory 目录内容清单, 避免 agent 浪费一步 `ls`

**`recall.go`**:
```go
func FindRelevantMemories(ctx context.Context, query string, memDir string, alreadySurfaced []string) ([]MemoryHeader, error)
```

- 扫描 memory 目录 → 生成 manifest
- 调用 small model sidequery:
  - System: "从清单中选择最多 5 个与 query 明显相关的记忆"
  - User: query + manifest + 最近使用的工具列表
  - 输出 schema: `{ selected_memories: []string }` (max 256 tokens)
- 过滤 alreadySurfaced (避免重复)

**`prompt.go`**:
```go
func BuildMemoryPrompt(memDir string) (string, error)
```

构建系统 prompt 段落:
1. 记忆目录路径
2. 保存指令 (两步: 写 topic 文件 → 更新 MEMORY.md 索引)
3. 类型说明
4. 不应保存的内容
5. 何时访问 (含漂移警告: 记忆可能过时)
6. MEMORY.md 内容

**`session_memory.go`**:
```go
type SessionMemory struct {
    Title        string
    CurrentState string
    TaskSpec     string
    Files        []string
    Workflow     string
    Errors       []string
    Learnings    []string
    KeyResults   []string
    Worklog      []string
}

func ExtractSessionMemory(messages []Message) *SessionMemory
func (sm *SessionMemory) Render() string
```

- 在 summarize 后重新注入到压缩上下文中
- 确保关键信息跨压缩保留

### 集成点

**修改 `internal/agent/prompt/prompt.go`**:
- 在系统 prompt 模板中增加 `{{.MemoryPrompt}}` 段落
- 用 `DiscoverContextFiles` 替代当前平面扫描

**修改 `internal/agent/session_agent.go`**:
- 对话循环结束后调用 `memdir.ExtractMemories` (goroutine)
- summarize 前调用 `ExtractSessionMemory`, summarize 后注入

---

## 阶段 4: 斜杠命令系统重构

> 依赖: 阶段 1-3 完成后才能实现部分命令

### 4.1 命令注册框架

**新建 `internal/commands/registry.go`**:

```go
type Registry struct {
    commands map[string]*RegisteredCommand
    aliases  map[string]string
}

type RegisteredCommand struct {
    Command Command
    Hidden  bool
}

func (r *Registry) Register(cmd Command)
func (r *Registry) Lookup(name string) (Command, bool)
func (r *Registry) All() []Command
func (r *Registry) Complete(prefix string) []Command
```

**新建 `internal/commands/types.go`**:

```go
type Command interface {
    Name() string
    Aliases() []string
    Description() string
    Args() []Arg // 参数定义
    Execute(ctx context.Context, args []string, env *CommandEnv) error
    Hidden() bool
}

type Arg struct {
    Name     string
    Required bool
    Variadic bool
}

type CommandEnv struct {
    Session    *session.Session
    ConfigStore *config.ConfigStore
    Agent      agent.Agent
    UI         UIBridge // 用于输出到 chat
}
```

**修改 `internal/ui/model/ui.go`**:

- textarea 中输入 `/` 后按 Enter:
  1. 解析命令名和参数
  2. 在 Registry 中查找
  3. 调用 `Execute`
  4. 结果显示在 chat 中
- `/` 输入时显示命令补全列表

### 4.2 核心命令清单

在 `internal/commands/builtin/` 下，每个命令一个文件:

| 文件 | 命令 | 别名 | 说明 |
|---|---|---|---|
| `compact.go` | `/compact` | | 带可选指令的 summarize |
| `clear.go` | `/clear` | `/reset`, `/new` | 清空当前会话 (原地) |
| `diff.go` | `/diff` | | 显示 `git diff` + 本轮文件变更 |
| `review.go` | `/review` | | PR 代码审查 (`gh pr diff` → agent 审查) |
| `commit.go` | `/commit` | | 分析 staged, 生成 message, `git commit` |
| `pr.go` | `/pr` | | commit + push + `gh pr create` |
| `security_review.go` | `/security-review` | | 安全审查 (`git diff` → 漏洞扫描) |
| `config.go` | `/config` | `/settings` | 打开外部编辑器编辑 crush.json |
| `doctor.go` | `/doctor` | | 诊断: git/rg/LSP/MCP/provider 连通性 |
| `cost.go` | `/cost` | | 本次会话 token 消耗和费用 |
| `status.go` | `/status` | | 版本/模型/账户/API 状态 |
| `copy.go` | `/copy` | | 复制最后回复到剪贴板 |
| `export.go` | `/export` | | 导出对话为 markdown |
| `rewind.go` | `/rewind` | `/checkpoint` | 回退到前一个 checkpoint |
| `rename.go` | `/rename` | | 重命名当前会话 |
| `context.go` | `/context` | | token 使用可视化 (按类别) |
| `memory.go` | `/memory` | | 用外部编辑器编辑 memory 文件 |
| `permissions_cmd.go` | `/permissions` | `/allowed-tools` | 列出/添加/删除权限规则 |
| `hooks_cmd.go` | `/hooks` | | 查看 hook 配置 |
| `mcp_cmd.go` | `/mcp` | | MCP 服务器管理 (列出/启用/禁用) |
| `add_dir.go` | `/add-dir` | | 添加额外工作目录 |
| `tasks.go` | `/tasks` | `/bashes` | 列出后台 bash 任务 |
| `vim.go` | `/vim` | | 切换 vim 模式 |
| `theme.go` | `/theme` | | 切换主题 |
| `skills_cmd.go` | `/skills` | | 列出可用 skills |
| `agents_cmd.go` | `/agents` | | agent 配置管理 |
| `feedback.go` | `/feedback` | `/bug` | 提交反馈 |
| `insights.go` | `/insights` | | 查询 SQLite stats 生成分析报告 |
| `pr_comments.go` | `/pr-comments` | | 获取 PR 评论 (`gh pr view --comments`) |
| `plugin.go` | `/plugin` | `/plugins` | 列出/启用/禁用 plugins |
| `advisor.go` | `/advisor` | | 配置 advisor 模型 |

---

## 阶段 5: 新工具补齐

> 依赖: 无 (独立)

### 5.1 NotebookEditTool

**新建 `internal/agent/tools/notebook_edit.go` + `notebook_edit.md`**:

```go
type NotebookEditParams struct {
    FilePath  string `json:"file_path"`
    CellIndex int    `json:"cell_index"`
    NewSource string `json:"new_source"`
    CellType  string `json:"cell_type,omitempty"` // "code" | "markdown"
}
```

- 解析 .ipynb JSON (标准 Jupyter notebook 格式)
- 修改指定 cell 的 source
- 支持插入/删除 cell

### 5.2 SnipTool (历史裁剪)

**新建 `internal/agent/tools/snip.go` + `snip.md`**:

```go
type SnipParams struct {
    MessageIDs []string `json:"message_ids,omitempty"`
    Range      string   `json:"range,omitempty"` // "last_n" 等
}
```

- 从对话历史中移除指定消息范围
- 释放上下文空间

### 5.3 SendMessageTool

**新建 `internal/agent/tools/send_message.go` + `send_message.md`**:

- 向特定 agent/worker 发送消息
- 需要 coordinator 支持 inter-agent 通信通道

### 5.4 VerifyPlanExecutionTool

**新建 `internal/agent/tools/verify_plan.go` + `verify_plan.md`**:

- 接收 plan 描述, 检查是否已正确执行
- 对比 plan 中的步骤与实际文件变更

### 注册

**修改 `internal/agent/coordinator.go`** `buildTools()` 和 `internal/config/config.go` `allToolNames()` 加入新工具。

---

## 阶段 6: Agent 系统增强

> 依赖: 阶段 1 (权限模式)

### 6.1 Advisor 模式

**新建 `internal/agent/advisor.go`**:

```go
type AdvisorService struct {
    model  string // 如 "opus"
    agent  fantasy.Agent
}

func (a *AdvisorService) Review(ctx context.Context, question string, transcript []Message) (string, error)
```

- 用更强模型审查当前策略/方案
- 输入: 当前对话 transcript + 问题
- 输出: 建议/策略

**修改 `internal/config/config.go`**:

Agents map 增加 `advisor` 定义:
```go
"advisor": {
    Model: "large", // 或专门配置
    AllowedTools: readOnlyTools,
}
```

### 6.2 Auto Mode (安全分类器)

**新建 `internal/permission/classifier.go`**:

```go
type Classifier struct {
    agent fantasy.Agent // small model
}

type ClassifyResult struct {
    ShouldBlock bool   `json:"shouldBlock"`
    Reason      string `json:"reason"`
}

func (c *Classifier) Classify(ctx context.Context, input ClassifyInput) (ClassifyResult, error)
```

两阶段分类:
1. **快速阶段**: max_tokens=64, 立即返回 block/allow
   - 如果 allow → 结束
   - 如果 block 或解析失败 → 进入阶段 2
2. **思考阶段**: max_tokens=4096, 带 chain-of-thought
   - 返回 `{shouldBlock, reason}`

Transcript 构建:
- 用户文本消息 → 包含
- assistant tool_use → 包含 (防 prompt injection, 排除 text)
- 序列化为紧凑 JSONL

**新建 `internal/permission/denial_tracking.go`**:

```go
type DenialTracker struct {
    consecutive int
    total       int
}

const (
    MaxConsecutiveDenials = 3
    MaxTotalDenials       = 20
)

func (t *DenialTracker) RecordDenial()
func (t *DenialTracker) RecordAllow()
func (t *DenialTracker) ShouldFallback() bool
```

### 6.3 多工作目录支持

**修改 `internal/config/config.go`**:

```go
type Options struct {
    // ... 现有
    AdditionalWorkingDirs []string `json:"additional_working_dirs,omitempty"`
}
```

**修改 `internal/permission/permission.go`**:

- `isWithinDir` 检查扩展为检查所有工作目录

---

## 阶段 7: UI 增强

> 依赖: 阶段 1, 4

### 7.1 主题系统

**新建 `internal/ui/theme/theme.go`**:

```go
type Theme struct {
    Name       string
    Primary    lipgloss.Color
    Secondary  lipgloss.Color
    Accent     lipgloss.Color
    Error      lipgloss.Color
    Warning    lipgloss.Color
    Success    lipgloss.Color
    Background lipgloss.Color
    Foreground lipgloss.Color
    Muted      lipgloss.Color
}

var themes = map[string]Theme{
    "default": { ... },
    "dark":    { ... },
    "light":   { ... },
    "monokai": { ... },
}
```

**修改 `internal/config/config.go`**: Options 增加 `Theme string`

### 7.2 Vim 模式

**新建 `internal/ui/vim/vim.go`**:

- 基本 motions: h/j/k/l, w/b/e, 0/$, gg/G
- 模式: Normal, Insert, Visual
- 操作: i/a/o (进入 insert), dd (删行), yy (复制行), p (粘贴), u (undo)
- 搜索: `/pattern`, n/N
- 集成到 Bubble Tea textarea

### 7.3 Context 可视化

**新建 `internal/ui/dialog/context.go`**:

- 显示 token 分布:
  - System prompt: X tokens (Y%)
  - Context files: X tokens (Y%)
  - Messages: X tokens (Y%)
  - Tool results: X tokens (Y%)
  - Available: X tokens (Y%)
- 彩色条形图

### 7.4 Diff 对话框

**新建 `internal/ui/dialog/diff_view.go`**:

- 显示 `git diff` (未暂存) + `git diff --cached` (已暂存)
- 本轮文件变更 (通过 filetracker)
- 语法高亮 (已有 Chroma 集成)

### 7.5 Cost 显示

- header/status bar 中显示累计 tokens 和估计费用
- 已有 token tracking 基础 (`session.TotalPromptTokens` 等)

### 7.6 权限建议 UI

**修改 `internal/ui/dialog/permission.go`**:

- 权限提示时在选项中增加 "Always allow [rule]" 选项
- 选择后调用 `ConfigStore.AddPermissionRule()`

---

## 阶段 8: Plugin 系统

> 依赖: 阶段 2 (hooks) + 阶段 4 (命令)

### 8.1 Plugin 类型与加载

**新建 `internal/plugins/types.go`**:

```go
type Plugin struct {
    Name     string
    Manifest PluginManifest
    Path     string
    Source   string // "user" | "project"
    Enabled  bool
}

type PluginManifest struct {
    Name        string            `json:"name"`
    Version     string            `json:"version,omitempty"`
    Description string            `json:"description,omitempty"`
    Author      *Author           `json:"author,omitempty"`
    Hooks       hooks.Settings    `json:"hooks,omitempty"`
    Commands    map[string]string `json:"commands,omitempty"` // name → 相对路径
    Agents      []string          `json:"agents,omitempty"`
    Skills      []string          `json:"skills,omitempty"`
    MCPServers  map[string]any    `json:"mcpServers,omitempty"`
}
```

**新建 `internal/plugins/loader.go`**:

```go
func LoadPlugin(dir string) (*Plugin, error)
```

1. 读取 `.crush-plugin/plugin.json` 或 `plugin.json`
2. 自动发现子目录: `commands/`, `agents/`, `skills/`, `hooks/`
3. 加载 hooks (从 `hooks/hooks.json` 或 manifest)
4. 验证 manifest

**新建 `internal/plugins/manager.go`**:

```go
type Manager struct {
    plugins []*Plugin
}

func (m *Manager) LoadAll(userDir, projectDir string) error
func (m *Manager) Enable(name string) error
func (m *Manager) Disable(name string) error
func (m *Manager) List() []*Plugin
func (m *Manager) Hooks() hooks.Settings   // 合并所有 plugin hooks
func (m *Manager) Commands() []Command      // 合并所有 plugin commands
func (m *Manager) MCPServers() map[string]any
```

### 8.2 Plugin 目录

- 用户: `~/.crush/plugins/`
- 项目: `.crush/plugins/`

### 8.3 集成

**修改 `internal/app/app.go`**:

- 启动时 `pluginManager.LoadAll()`
- 合并 plugin hooks 到 hooks snapshot
- 合并 plugin commands 到 command registry
- 合并 plugin MCP servers 到 MCP manager

---

## 阶段 9: 高级特性

> 依赖: 阶段 4

### 9.1 Doctor 诊断

检查清单:
- [ ] Go 版本
- [ ] git 可用 + 版本
- [ ] ripgrep (rg) 可用
- [ ] 环境变量 (API keys 是否设置)
- [ ] crush.json 语法有效性
- [ ] 每个配置的 LSP server 是否可启动
- [ ] 每个配置的 MCP server 是否可连接
- [ ] 每个配置的 provider 是否可 ping
- [ ] CLAUDE.md / AGENTS.md 文件大小警告 (>40KB)
- [ ] 磁盘空间 (数据目录)

### 9.2 Rewind (检查点)

**新建 `internal/history/checkpoint.go`**:

```go
type Checkpoint struct {
    ID        string
    Timestamp time.Time
    MessageID string // 对话中的位置
    Files     map[string][]byte // 文件快照 (仅已修改的)
}

func SaveCheckpoint(session *Session, files []string) (*Checkpoint, error)
func RestoreCheckpoint(cp *Checkpoint) error
```

- 每轮对话前自动 save checkpoint
- `/rewind` 列出 checkpoints, 选择后:
  1. 回退对话历史到该位置
  2. 恢复文件内容

### 9.3 Export/Copy

- `/export [file]`: 将对话渲染为 markdown, 写入文件或输出到 stdout
- `/copy`: 取最后一条 assistant 消息, 用 `pbcopy`/`xclip`/`xsel`/PowerShell 复制

### 9.4 Git 工作流

- `/commit`:
  1. `git status` + `git diff --cached`
  2. 用 agent 生成 commit message
  3. 显示给用户确认
  4. `git commit -m "..."`
- `/pr`:
  1. 执行 `/commit` 流程
  2. `git push -u origin HEAD`
  3. `gh pr create --title "..." --body "..."`
- `/review [url]`:
  1. `gh pr diff [url]`
  2. 将 diff 交给 agent 审查
  3. 输出审查结果

### 9.5 Insights

- 查询 SQLite stats 表
- 生成报告: 总 token、总费用、按模型/按天/按工具分布
- 渲染为 chat 中的表格

---

## 阶段 10: 远程/高级特性 (长期)

> 依赖: 各自独立

### 10.1 Voice 模式
- STT 集成 (Whisper API 或 Deepgram)
- 麦克风输入 → 转文字 → 作为用户消息
- `/voice` 命令切换

### 10.2 Remote/Bridge
- WebSocket 服务器/客户端
- 序列化/反序列化消息流
- JWT 认证
- `crush remote` 子命令

### 10.3 IDE 集成
- 作为 MCP server 运行 (`crush mcp-server`)
- VS Code / JetBrains 通过 MCP SSE-IDE transport 连接
- 暴露所有内置工具

### 10.4 Daemon/后台会话
- `crush daemon` — 后台长驻进程
- `crush ps` — 列出活跃会话
- `crush attach <id>` — 连接到后台会话
- `crush kill <id>` — 终止会话
- `--bg` 标志 — 启动即后台

### 10.5 Auto-updater
- `crush update` — 检查+下载+替换自身
- 通道: stable / latest
- GitHub Release API

---

## 执行顺序与依赖图

```
阶段 1 (权限) ──┬── 阶段 2 (hooks) ──┬── 阶段 8 (插件)
               │                    │
               ├── 阶段 6 (agent)   │
               │                    │
阶段 3 (记忆) ──┤                    │
               │                    │
               └── 阶段 4 (命令) ───┴── 阶段 9 (高级)
                       │
阶段 5 (工具) ──────────┤
                       │
                   阶段 7 (UI)

阶段 10 (远程) ← 独立, 最后
```

可并行的组合:
- 阶段 1 + 阶段 3 + 阶段 5
- 阶段 2 + 阶段 3 (后半)
- 阶段 4 + 阶段 5

---

## 参考文件

| Claude Code 源码位置 | 对应功能 |
|---|---|
| `utils/permissions/` (~24 文件) | 权限系统 |
| `utils/hooks.ts` + `utils/hooks/` | Hooks 系统 |
| `schemas/hooks.ts` | Hook 配置 schema |
| `utils/claudemd.ts` | CLAUDE.md 加载/遍历/@include |
| `utils/frontmatterParser.ts` | Frontmatter 解析 |
| `memdir/` | 自动记忆系统 |
| `services/extractMemories/` | 记忆提取 |
| `services/autoDream/` | 记忆合并 |
| `services/SessionMemory/` | Session 记忆 |
| `commands/` (~60 目录) | 斜杠命令 |
| `tools/` (~40 目录) | Agent 工具 |
| `utils/plugins/pluginLoader.ts` | Plugin 加载 |
| `utils/plugins/schemas.ts` | Plugin manifest schema |
| `types/permissions.ts` | 权限类型定义 |
| `types/plugin.ts` | Plugin 类型定义 |

## 验证策略

每个阶段完成后:
1. `task lint`
2. `task test`
3. `task build`
4. 手动启动 `crush` 测试新功能
