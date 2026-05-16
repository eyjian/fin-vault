// 数据导出：直接拼 URL，由浏览器下载

export function exportFileURL(format: 'xlsx' | 'md', scope: 'holdings' | 'transactions' | 'full', start?: string, end?: string) {
  const qs = new URLSearchParams()
  qs.set('format', format)
  qs.set('scope', scope)
  if (start) qs.set('start', start)
  if (end) qs.set('end', end)
  return `/api/v1/export?${qs.toString()}`
}
