import { apiClient } from '@/api/client'

export interface ContentMonitorConfig {
  emails: string[]
  config_version: number
  updated_at: string
}

export interface ContentMonitorItem {
  id: number
  user_email: string
  model: string
  content: string
  created_at: string
}

export interface ContentMonitorPage {
  items: ContentMonitorItem[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface ContentMonitorDeleteResult {
  deleted_events: number
  deleted_jobs: number
}

const basePath = '/admin/content-monitor'

export async function getContentMonitorConfig(): Promise<ContentMonitorConfig> {
  const { data } = await apiClient.get<ContentMonitorConfig>(`${basePath}/config`)
  return data
}

export async function saveContentMonitorConfig(emails: string[]): Promise<ContentMonitorConfig> {
  const { data } = await apiClient.put<ContentMonitorConfig>(`${basePath}/config`, { emails })
  return data
}

export async function listContentMonitorEvents(page: number, pageSize: number): Promise<ContentMonitorPage> {
  const { data } = await apiClient.get<ContentMonitorPage>(`${basePath}/events`, { params: { page, page_size: pageSize } })
  return data
}

export async function deleteContentMonitorEvents(): Promise<ContentMonitorDeleteResult> {
  const { data } = await apiClient.delete<ContentMonitorDeleteResult>(`${basePath}/events`)
  return data
}
