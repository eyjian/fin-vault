import { get } from './http'
import type { Platform } from './types'

export const platformApi = {
  list() {
    return get<Platform[]>('/platforms')
  }
}
