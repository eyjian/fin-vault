import axios from 'axios'
import type { AxiosInstance, AxiosRequestConfig } from 'axios'
import { ElMessage } from 'element-plus'

const baseURL = '/api/v1'

export interface ApiResp<T = unknown> {
  code: number
  message: string
  data: T
  request_id?: string
}

export interface PageResp<T> {
  list?: T[]
  items?: T[]
  total: number
  page: number
  size: number
}

const instance: AxiosInstance = axios.create({
  baseURL,
  timeout: 30000
})

// 注入默认 X-User-Id；多用户阶段可改为从 store 取
instance.interceptors.request.use((config) => {
  config.headers = config.headers || {}
  if (!config.headers['X-User-Id']) {
    config.headers['X-User-Id'] = '1'
  }
  return config
})

// 统一拆包：成功直接返回 data；失败弹消息并抛出
instance.interceptors.response.use(
  (resp) => {
    const body = resp.data as ApiResp
    if (body && typeof body.code === 'number' && body.code !== 0) {
      ElMessage.error(`[${body.code}] ${body.message || '请求失败'}`)
      return Promise.reject(body)
    }
    return resp
  },
  (err) => {
    const msg = err?.response?.data?.message || err?.message || '网络错误'
    ElMessage.error(msg)
    return Promise.reject(err)
  }
)

export async function get<T>(url: string, params?: Record<string, unknown>, config?: AxiosRequestConfig): Promise<T> {
  const resp = await instance.get<ApiResp<T>>(url, { params, ...config })
  return resp.data.data
}

export async function post<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
  const resp = await instance.post<ApiResp<T>>(url, data, config)
  return resp.data.data
}

export async function put<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
  const resp = await instance.put<ApiResp<T>>(url, data, config)
  return resp.data.data
}

export async function patch<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
  const resp = await instance.patch<ApiResp<T>>(url, data, config)
  return resp.data.data
}

export async function del<T = unknown>(url: string, config?: AxiosRequestConfig): Promise<T> {
  const resp = await instance.delete<ApiResp<T>>(url, config)
  return resp.data.data
}

export default instance
