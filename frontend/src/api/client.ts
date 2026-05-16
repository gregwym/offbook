import axios from 'axios'

// Vite proxies /api → backend in dev (see vite.config.ts). In other
// environments (preview build, prod, container) set VITE_API_BASE_URL.
const baseURL = import.meta.env.VITE_API_BASE_URL ?? '/api/v1'

export const apiClient = axios.create({
  baseURL,
  headers: { 'Content-Type': 'application/json' },
  withCredentials: false,
})

// Standard backend envelope shapes (mirror backend handler conventions).
export type ApiList<T> = { data: T[]; total: number }
export type ApiItem<T> = { data: T }
export type ApiError = { error: string; code?: string }
