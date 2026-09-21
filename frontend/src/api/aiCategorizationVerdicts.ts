import { apiClient, type ApiList } from './client'
import type { AICategorizationVerdict } from '../types/aiCategorizationVerdict'

export async function listCategorizationVerdicts(): Promise<AICategorizationVerdict[]> {
  const res = await apiClient.get<ApiList<AICategorizationVerdict>>('/categorization-verdicts')
  return res.data.data
}
