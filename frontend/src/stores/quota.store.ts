
import { Injectable } from '@angular/core';
import { BehaviorSubject } from 'rxjs';
import { listQuotaUsage } from '../api/quota';
import type { QuotaUsage } from '../types/domain';

interface QuotaState { usage: QuotaUsage[]; loading: boolean; error: string }
const initialState: QuotaState = { usage: [], loading: false, error: '' };

// QuotaStore holds the annual permit quota usage snapshot shared by the
// generator and manifest pages and refreshes it whenever manifests change.
@Injectable({ providedIn: 'root' })
export class QuotaStore {
  private readonly subject = new BehaviorSubject<QuotaState>(initialState);
  readonly state$ = this.subject.asObservable();
  get snapshot(): QuotaState { return this.subject.value; }

  async load(year?: number): Promise<void> {
    this.patch({ loading: true, error: '' });
    try {
      const result = await listQuotaUsage(year);
      this.subject.next({ usage: result.data, loading: false, error: '' });
    } catch (error) {
      this.patch({ loading: false, error: error instanceof Error ? error.message : String(error) });
    }
  }

  byGenerator(code: string | undefined, year: number): QuotaUsage | undefined {
    if (!code) return undefined;
    return this.snapshot.usage.find((item) =>
      item.generatorCode.toUpperCase() === code.toUpperCase() && item.year === year);
  }

  private patch(value: Partial<QuotaState>): void {
    this.subject.next({ ...this.subject.value, ...value });
  }
}
