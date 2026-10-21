export type Category = {
  id: number
  // user_id is set on a category the user created (#573); null/absent on
  // the built-in system taxonomy.
  user_id?: number | null
  parent_id?: number | null
  name: string
  slug: string
  icon?: string | null
  color?: string | null
  is_system: boolean
  created_at: string
  updated_at: string
}
