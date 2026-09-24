
import { CommonModule } from '@angular/common';
import { Component, Input } from '@angular/core';
import type { QuotaUsage } from '../../types/domain';

// QuotaPanel renders the annual permit quota usage for generator and manifest
// pages. mode="generator" shows every generator on the page; mode="manifest"
// shows only generators linked by manifests currently displayed.
@Component({
  selector: 'app-quota-panel',
  standalone: true,
  imports: [CommonModule],
  template: `
    <section class="quota-panel" aria-label="年度许可额度">
      <header>
        <strong>年度许可额度使用</strong>
        <span>{{ rows.length }} 个单位 · 自然年 {{ year }}</span>
      </header>
      <div *ngIf="rows.length; else empty" class="quota-grid">
        <article *ngFor="let row of rows">
          <div class="quota-head">
            <strong>{{ mode === 'generator' ? row.generatorCode : row.generatorCode }}</strong>
            <span class="quota-year">{{ row.year }}</span>
          </div>
          <small class="quota-name">{{ row.generatorName }}</small>
          <div class="quota-bar" [class.quota-bar--over]="row.exceeded">
            <i [style.width.%]="percent(row)" [class.over]="row.exceeded" [class.warn]="!row.exceeded && percent(row) >= 90"></i>
          </div>
          <small class="quota-figures">
            已用 <strong [class.text-over]="row.exceeded">{{ row.usedKg | number: '1.0-2' }}</strong> /
            上限 {{ row.annualQuotaKg | number: '1.0-2' }} kg ·
            剩余 <span [class.text-over]="row.exceeded">{{ row.remainingKg | number: '1.0-2' }}</span> kg
            <ng-container *ngIf="row.exceeded"> · 超出 <span class="text-over">{{ row.exceededKg | number: '1.0-2' }}</span> kg</ng-container>
          </small>
        </article>
      </div>
      <ng-template #empty><div class="empty">暂无额度数据</div></ng-template>
    </section>
  `
})
export class QuotaPanelComponent {
  @Input() rows: QuotaUsage[] = [];
  @Input() year = new Date().getUTCFullYear();
  @Input() mode: 'generator' | 'manifest' = 'generator';

  percent(row: QuotaUsage): number {
    if (row.annualQuotaKg <= 0) return 100;
    return Math.min(100, Math.round((row.usedKg / row.annualQuotaKg) * 100));
  }
}
