import { useState } from 'react'
import { useFormStatus } from 'react-dom'

// A field's error hides once the field changes (typed, or a controlled `value` set by the form) and while its
// form submits, then shows again with the next result, so a corrected value doesn't keep the old message.
// Returns the error to show and an edit callback.
export function useFieldError(error?: string | null, value?: unknown) {
  const { pending } = useFormStatus()
  const [edited, setEdited] = useState(false)
  const [prev, setPrev] = useState(error)
  const [prevValue, setPrevValue] = useState(value)
  if (pending && edited) setEdited(false)
  if (value !== prevValue) {
    setPrevValue(value)
    setEdited(true)
  }
  if (error !== prev) {
    setPrev(error)
    setEdited(false)
  }
  return [edited || pending ? null : error, () => setEdited(true)] as const
}
