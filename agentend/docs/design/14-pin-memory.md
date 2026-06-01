# Pin Memory — 约束钉住与上下文注入

## 实现了什么

Pin Memory 系统允许用户将关键约束"钉住"，持久化为 Markdown 文件并自动生成摘要。Orchestrator 在规划时读取 pinned 内容作为约束注入到 LLM 上下文中。

## 怎么实现的

### PinMemory (`src/orchestrator/memory/pin_memory.py`)

管理 `common/` 目录下的 pin 文件和 `_pins.yaml` 索引：

```python
class PinMemory:
    def __init__(self, common_dir: str | Path) -> None:
        self.common_dir = Path(common_dir)
```

核心方法：

```python
async def pin(self, title: str, content: str, source: str = "user") -> str
async def pin_existing(self, filename: str, title: str = "", source: str = "user") -> bool
def unpin(self, filename: str) -> bool
def get_context(self) -> str
def get_full_content(self, filename: str) -> str | None
def list_pins(self) -> list[dict]
```

### Pin 操作

`pin(title, content)` 执行：
1. 将内容写入 `common/{slugify(title)}.md`
2. 调用 LLM 生成 1-3 句摘要（使用配置的 `llm.model`）
3. 追加记录到 `_pins.yaml`

`pin_existing(filename, title)` 对已有文件执行类似流程。

`unpin(filename)` 从 `_pins.yaml` 中移除条目（文件保留）。

### 上下文注入

`get_context()` 生成结构化约束文本，供 Orchestrator 注入到规划 prompt：

```python
def get_context(self) -> str:
    pins = self._load_pins()
    if not pins:
        return ""
    lines = ["## 必须遵守的约束（Pin）", ""]
    for p in pins:
        lines.append(f"- **{p['title']}**: {p['summary']}")
        lines.append(f"  > 完整内容: common/{p['filename']}")
    return "\n".join(lines)
```

### _pins.yaml 格式

```yaml
- filename: build-constraints.md
  title: Build Constraints
  source: user
  pinned_at: "2026-05-21T12:00:00+00:00"
  summary: 项目必须使用 pnpm 构建，禁止 npm...
```

### 与 API 集成

通过 `src/api/v1/pin.py` 暴露 REST 端点，前端可 pin/unpin/list 操作。
