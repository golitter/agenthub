import { useQuery } from '@tanstack/react-query'

import { fetchTask } from '@/lib/api'

export function useTask(taskId: string) {
  return useQuery({
    queryKey: ['task', taskId],
    queryFn: () => fetchTask(taskId),
    enabled: !!taskId,
  })
}

export function useSessions(taskId: string) {
  const { data, ...rest } = useTask(taskId)
  return {
    ...rest,
    data: data?.sessions ?? [],
    task: data?.task,
  }
}
