import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import type { AgentType } from '@/generated/request'
import { createConversation, fetchConversations } from '@/lib/api'

export function useConversations() {
  return useQuery({
    queryKey: ['conversations'],
    queryFn: fetchConversations,
  })
}

export function useCreateConversation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({
      agentType,
      agentName,
      title,
      repoPath,
    }: {
      agentType: AgentType
      agentName?: string
      title?: string
      repoPath?: string
    }) => createConversation(agentType, agentName, title, repoPath),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['conversations'] })
    },
  })
}
