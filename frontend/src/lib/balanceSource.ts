// Who reported an account balance, in the user's words rather than the
// stored source enum (#665). Accounts and Reconciliation share it so one
// observation reads the same on both pages.

type Reporter = 'you' | 'bank' | 'statement' | 'offbook' | 'unknown'

function reporter(source: string | null | undefined): Reporter {
  switch (source) {
    case 'manual':
      return 'you'
    case 'plaid':
      return 'bank'
    case 'csv':
    case 'pdf':
    case 'photo':
      return 'statement'
    case 'system':
      return 'offbook'
    default:
      return 'unknown'
  }
}

// sourceLabel is the reporter as a noun: "You", "Your bank".
export function sourceLabel(source: string | null | undefined): string {
  return {
    you: 'You',
    bank: 'Your bank',
    statement: 'Your statement',
    offbook: 'Offbook',
    unknown: 'Reported',
  }[reporter(source)]
}

// reportedBy completes "Reported <when> …": "by you", "from your statement".
// Empty for an unknown source, so the line reads just "Reported <when>".
export function reportedBy(source: string | null | undefined): string {
  return {
    you: 'by you',
    bank: 'by your bank',
    statement: 'from your statement',
    offbook: 'by Offbook',
    unknown: '',
  }[reporter(source)]
}
