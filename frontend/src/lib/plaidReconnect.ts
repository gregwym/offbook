// Where a reauth_required bank gets fixed (#641): Settings → Linked
// Institutions, which owns the Plaid Link update-mode Reconnect flow (#364).
// The banner and the Accounts pill both link here; SettingsPage scrolls the
// section into view when the hash matches.
export const LINKED_INSTITUTIONS_ID = 'linked-institutions'
export const RECONNECT_PATH = `/settings#${LINKED_INSTITUTIONS_ID}`
