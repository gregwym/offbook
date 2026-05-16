type Props = {
  title: string
  description?: string
}

export function PageStub({ title, description }: Props) {
  return (
    <div>
      <h1 className="text-2xl font-semibold text-gray-900">{title}</h1>
      <p className="mt-2 text-sm text-gray-500">
        {description ?? 'Coming in a later milestone.'}
      </p>
    </div>
  )
}
