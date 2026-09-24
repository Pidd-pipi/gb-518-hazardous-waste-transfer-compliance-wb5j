import { CommonModule } from '@angular/common';
import { Component, Input } from '@angular/core';
import type { QuotaUsage } from '../../types/domain';

@Component({
  selector: 'app-quota-usage',
  standalone: true,
  imports: [CommonModule],
  template: `
    <div class="quota-usage" *ngIf="usage" [class.quota-usage--over]="isOver()" [class.quota-usage--tight]="isTight()" role="group" [attr.aria-label]="label()">
      <div class="quota-usage__line">
        <span>{{ usage.generatorCode }} · {{ usage.year }} 年度许可额度</span>
        <strong>{{ formatKg(usage.usedKg) }} / {{ formatKg(usage.annualQuotaKg) }} kg</strong>
      </div>
      <div class="quota-usage__bar" aria-hidden="true">
        <i [style.width.%]="percent()"></i>
      </div>
      <small>
        已用 {{ formatKg(usage.usedKg) }} kg ·
        剩余 <strong>{{ formatKg(usage.remainingKg) }} kg</strong>
        <ng-content></ng-content>
      </small>
    </div>
  `,
  styles: [`
    .quota-usage { display: grid; gap: 5px; min-width: 190px; color: #284b60; font-size: 12px; }
    .quota-usage__line { display: flex; justify-content: space-between; gap: 10px; }
    .quota-usage__line span { color: #6c7e8a; }
    .quota-usage__line strong { font-weight: 700; white-space: nowrap; }
    .quota-usage__bar { height: 6px; background: #e5ebee; overflow: hidden; }
    .quota-usage__bar i { display: block; height: 100%; background: #2a9d78; transition: width .2s ease; }
    .quota-usage--tight .quota-usage__bar i { background: #d69a28; }
    .quota-usage--over .quota-usage__bar i { background: #c84855; }
    .quota-usage--over small strong { color: #9d2c37; }
    small { color: #6c7e8a; }
  `]
})
export class QuotaUsageComponent {
  @Input({ required: true }) usage: QuotaUsage | null | undefined;
  /** Optional weight of the manifest being shown; appended via ng-content is
   * handled by the parent, this just colors the bar. */
  @Input() requestedKg?: number;

  formatKg(value: number): string {
    return Number.isInteger(value) ? String(value) : value.toFixed(1);
  }

  percent(): number {
    if (!this.usage || this.usage.annualQuotaKg <= 0) return 0;
    return Math.min(100, Math.round((this.usage.usedKg / this.usage.annualQuotaKg) * 100));
  }

  isOver(): boolean {
    if (!this.usage) return false;
    if (this.requestedKg != null && this.usage.usedKg + this.requestedKg > this.usage.annualQuotaKg) return true;
    return this.usage.usedKg > this.usage.annualQuotaKg;
  }

  isTight(): boolean {
    if (!this.usage || this.isOver()) return false;
    const requested = this.requestedKg ?? 0;
    return this.usage.remainingKg - requested < this.usage.annualQuotaKg * 0.1;
  }

  label(): string {
    return this.usage
      ? `${this.usage.generatorCode} ${this.usage.year} 年度额度已用 ${this.usage.usedKg} 千克，剩余 ${this.usage.remainingKg} 千克`
      : '';
  }
}
