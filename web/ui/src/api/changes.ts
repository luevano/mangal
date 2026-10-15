import { fetchJSON } from './client'
import type { ApplyFields, ChangeResponse } from './schemas'

// change is a PlanSpec's run for a plan/apply endpoint of the API v1 (x-mangal-plan): the
// body planned as is, applied with the fields (expect, force, confirm).
export function change(method: 'POST' | 'PUT' | 'DELETE', path: string, body: object = {}) {
  return async (apply: boolean, fields: ApplyFields): Promise<ChangeResponse> => {
    const url = `/api/v1${path}${apply ? '?apply=1' : ''}`
    const { data } = await fetchJSON<ChangeResponse>(url, {
      method,
      body: JSON.stringify(apply ? { ...body, ...fields } : body)
    })
    return data
  }
}
