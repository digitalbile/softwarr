import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, isConflict } from '../../api/client'

// Runs an admin action on a request. A 409 means the request moved on
// without us, so show the server's message and refetch.
export function useRequestAction() {
  const queryClient = useQueryClient()
  const [conflict, setConflict] = useState('')

  const perform = async (id: number, action: string, json?: unknown) => {
    setConflict('')
    try {
      await api(`/requests/${id}/${action}`, { method: 'POST', json })
    } catch (err) {
      if (isConflict(err)) setConflict(err.message)
      throw err
    } finally {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['requests'] }),
        queryClient.invalidateQueries({ queryKey: ['activity'] }),
      ])
    }
  }
  return { perform, conflict }
}
