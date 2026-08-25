export function isAdminQueryKey(queryKey: readonly unknown[]): boolean {
  const root = queryKey[0]
  return typeof root === 'string' && root.startsWith('admin-')
}
