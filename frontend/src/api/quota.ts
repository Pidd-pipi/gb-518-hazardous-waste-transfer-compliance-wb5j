
import { request } from './client';
import type { QuotaUsage } from '../types/domain';

// Annual permit quota usage is aggregated server-side from submitted,
// in-transit and received manifests. The year parameter is optional and
// defaults to the current natural year on the backend.
export async function listQuotaUsage(year?: number) {
  const suffix = year ? `?year=${year}` : '';
  return request<QuotaUsage[]>(`/quota-usage${suffix}`);
}
