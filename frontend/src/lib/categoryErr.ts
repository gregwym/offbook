// categoryErr turns a category create/rename failure into a sentence the user
// can act on; anything unrecognised falls back to the server's message.
export function categoryErr(err: unknown, name: string): string {
  const code = (err as { response?: { data?: { code?: string } } })?.response?.data?.code
  switch (code) {
    case 'CATEGORY_NAME_TAKEN':
      return `“${name}” already exists. Pick a different name.`
    case 'INVALID_NAME':
      return 'Use a name between 1 and 60 characters.'
    case 'INVALID_PARENT':
      return 'That group is no longer available. Pick another or choose None.'
    default: {
      const r = (err as { response?: { data?: { error?: string } } })?.response
      const msg = r?.data?.error ?? (err instanceof Error ? err.message : 'request failed')
      return msg.charAt(0).toUpperCase() + msg.slice(1)
    }
  }
}
