import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import type { AgentType } from '@/generated/request'
import { type Conversation, createConversation, fetchConversations } from '@/lib/api'
import { queryKeys, upsertConversation } from '@/lib/query-keys'

export function useConversations(options: { enabled?: boolean } = {}) {
  return useQuery({
    queryKey: queryKeys.conversations,
    queryFn: fetchConversations,
    enabled: options.enabled ?? true,
  })
}

export function useCreateConversation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      agents: { type: AgentType; name: string }[]
      repoPath?: string
      title?: string
    }) => createConversation(params.agents, params.repoPath, params.title),
    onSuccess: (conversation: Conversation) => {
      // 先把新行放进缓存，避免用户在创建成功后的刷新窗口内发送首条消息
      // 时，stream optimistic patch 找不到目标会话。
      queryClient.setQueryData<Conversation[]>(queryKeys.conversations, (current) =>
        upsertConversation(current, conversation),
      )
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations })
    },
  })
}
