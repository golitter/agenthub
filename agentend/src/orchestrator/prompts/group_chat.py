"""用于跨 Agent 记忆的群聊上下文提示词模板。

将窗口消息（该 Agent 上次发言后其他 Agent 发出的消息）
格式化为通过 GroupChatRule 注入的系统提示词片段。
"""

GROUP_CHAT_CONTEXT = """\
## 群聊上下文

你正在参与一个多 Agent 协作群聊。以下是你上次发言后，其他成员发出的消息。
请参考这些内容来执行你的任务——了解当前进展、避免重复工作、与其他成员协作。

{messages}
"""


def build_group_chat_context(cross_round_messages: list[dict] | None = None) -> str:
    """将跨 Agent 的窗口消息格式化为提示词片段。

    Args:
        cross_round_messages: 消息 dict 的列表，包含以下键：
            role (str): "user" 或 "agent"
            agent_name (str): Agent 的名称
            content (str): 消息内容（已由 backend 截断）

    Returns:
        格式化后的提示词字符串；若无有效消息则返回空字符串。
    """
    if not cross_round_messages:
        return ""

    lines: list[str] = []
    for msg in cross_round_messages:
        role = msg.get("role", "")
        name = msg.get("agent_name", "")
        content = msg.get("content", "")
        if role == "user":
            lines.append(f"👤 用户:\n{content}")
        elif role == "agent" and name:
            lines.append(f"🤖 {name}:\n{content}")

    if not lines:
        return ""

    return GROUP_CHAT_CONTEXT.format(messages="\n\n".join(lines))
