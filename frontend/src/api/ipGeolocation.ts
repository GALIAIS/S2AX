import { apiClient } from './client'

export type IPGeolocationLookupStatus = 'success' | 'private' | 'invalid' | 'unavailable' | 'not_found'

export interface IPGeolocationLookupResult {
  ip: string
  status: IPGeolocationLookupStatus
  country?: string
  country_code?: string
  region?: string
  city?: string
  organization?: string
}

export async function lookupIPGeolocation(ips: string[]): Promise<IPGeolocationLookupResult[]> {
  const { data } = await apiClient.post<IPGeolocationLookupResult[]>('/ip-geolocation/lookup', { ips })
  return data
}
