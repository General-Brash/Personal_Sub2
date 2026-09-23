/**
 * Admin Model Plaza API endpoints
 * 广场展示管理：逐模型隐藏 / 置顶 / 排序 + “无账号支持”收敛开关，版本乐观锁写入。
 */

import { apiClient } from '../client'
import type { ModelPlazaAvailabilityState } from '@/api/modelPlaza'

/** 管理面板视角的单个模型（含被隐藏项，便于管理员查看自己隐藏了什么）。 */
export interface ModelPlazaAdminModel {
  /** 归一化键 "platform:model_id"，PUT 时原样回传。 */
  key: string
  model_id: string
  display_name: string
  platform: string
  availability_state: ModelPlazaAvailabilityState
  hidden: boolean
  pinned: boolean
  sort_order: number
}

/** 逐模型展示覆盖。 */
export interface ModelPlazaAdminOverride {
  hidden: boolean
  pinned: boolean
  sort_order: number
}

export interface ModelPlazaAdminSettings {
  models: ModelPlazaAdminModel[]
  /** 默认 true：收敛掉“无任何账号支持”的幽灵模型；关闭则回退展示全部播种。 */
  hide_no_account: boolean
  /** 内容寻址版本，写入需回传以做乐观锁。 */
  version: string
}

export interface UpdateModelPlazaAdminRequest {
  overrides: Record<string, ModelPlazaAdminOverride>
  hide_no_account: boolean
  version: string
}

/** 读取广场展示管理设置（含全部模型）。 */
export async function getModelPlazaAdmin(options?: { signal?: AbortSignal }): Promise<ModelPlazaAdminSettings> {
  const { data } = await apiClient.get<ModelPlazaAdminSettings>('/admin/model-plaza', { signal: options?.signal })
  return data
}

/** 以版本乐观锁写入广场展示管理设置，返回写入后的最新状态。 */
export async function updateModelPlazaAdmin(request: UpdateModelPlazaAdminRequest): Promise<ModelPlazaAdminSettings> {
  const { data } = await apiClient.put<ModelPlazaAdminSettings>('/admin/model-plaza', request)
  return data
}

export const modelPlazaAdminAPI = { getModelPlazaAdmin, updateModelPlazaAdmin }
export default modelPlazaAdminAPI
