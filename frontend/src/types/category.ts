export type Category = {
  id: number
  parent_id?: number | null
  name: string
  slug: string
  icon?: string | null
  color?: string | null
  is_system: boolean
  created_at: string
  updated_at: string
}
