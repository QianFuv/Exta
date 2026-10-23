/** Logical-call counts use local metadata only, never upstream costs or balances. */
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import type { Schemas } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { Button, Disclosure, Select } from "../../components/Controls";
import { EmptyState, QueryView } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { DataTable } from "../../components/DataTable";

/** Display bounded time ranges without counting retries as new user calls. */
export function CallTrend() {
  const { t } = useI18n();
  const api = useApi();
  const [hours, setHours] = useState<24 | 168 | 720>(24);
  const query = useQuery({
    queryKey: ["call-trends", hours],
    queryFn: () => call(api.GET("/v1/admin/usage/trends", { params: { query: { hours } } })),
    refetchInterval: 30_000,
  });
  return (
    <Section
      title={t("trends.title")}
      description={t("trends.coverage")}
      actions={
        <>
          <Select
            aria-label={t("trends.range")}
            value={String(hours)}
            onChange={(event) => setHours(Number(event.target.value) as 24 | 168 | 720)}
          >
            <option value="24">{t("trends.day")}</option>
            <option value="168">{t("trends.week")}</option>
            <option value="720">{t("trends.month")}</option>
          </Select>
          <Button disabled={query.isFetching} onClick={() => void query.refetch()}>
            {t("common.refresh")}
          </Button>
        </>
      }
    >
      <QueryView query={query}>{(data) => <TrendPlot data={data} />}</QueryView>
    </Section>
  );
}

/** Render an accessible SVG with identical count data available as a semantic table. */
function TrendPlot({ data }: { data: Schemas["CallTrends"] }) {
  const { t, locale, formatNumber } = useI18n();
  const series = ["total", "succeeded", "failed"] as const;
  const totals = data.buckets.reduce(
    (sum, bucket) => ({
      total: sum.total + bucket.total,
      pending: sum.pending + bucket.pending,
      uncertain: sum.uncertain + bucket.uncertain,
      cancelled: sum.cancelled + bucket.cancelled,
    }),
    { total: 0, pending: 0, uncertain: 0, cancelled: 0 },
  );
  const maximum = Math.max(
    4,
    Math.ceil(Math.max(...data.buckets.map((bucket) => bucket.total), 0) / 4) * 4,
  );
  const x = (index: number) => 60 + (index / Math.max(1, data.buckets.length - 1)) * 760;
  const y = (count: number) => 240 - (count / maximum) * 200;
  const time = (value: string) =>
    new Intl.DateTimeFormat(locale === "zh" ? "zh-CN" : "en-US", {
      timeZone: "UTC",
      month: "2-digit",
      day: "2-digit",
      ...(data.bucket_seconds === 3600
        ? { hour: "2-digit" as const, minute: "2-digit" as const, hourCycle: "h23" as const }
        : {}),
    }).format(new Date(value));
  return (
    <>
      <ul className="trend-legend" aria-label={t("trends.title")}>
        {series.map((name) => (
          <li key={name} className={`trend-${name}`}>
            <span aria-hidden className="trend-swatch" />
            {t(`trends.${name}`)}{" "}
            <strong>
              {formatNumber(data.buckets.reduce((sum, bucket) => sum + bucket[name], 0))}
            </strong>
          </li>
        ))}
      </ul>
      <div className="trend-chart">
        <svg
          viewBox="0 0 860 290"
          role="img"
          aria-label={`${t("trends.title")}: ${time(data.from)} – ${time(data.to)}, UTC; ${t("trends.total")}: ${totals.total}`}
        >
          <text x="60" y="18" className="trend-axis-label">
            {t("trends.count")}
          </text>
          {[0, 1, 2, 3, 4].map((tick) => (
            <g key={tick}>
              <line
                className="trend-grid"
                x1="60"
                x2="820"
                y1={y((tick * maximum) / 4)}
                y2={y((tick * maximum) / 4)}
              />
              <text
                className="trend-axis-label"
                x="48"
                y={y((tick * maximum) / 4) + 4}
                textAnchor="end"
              >
                {formatNumber((tick * maximum) / 4)}
              </text>
            </g>
          ))}
          {[0, Math.floor((data.buckets.length - 1) / 2), data.buckets.length - 1].map((index) =>
            data.buckets[index] ? (
              <text
                key={index}
                className="trend-axis-label"
                x={x(index)}
                y="266"
                textAnchor={
                  index === 0 ? "start" : index === data.buckets.length - 1 ? "end" : "middle"
                }
              >
                {time(data.buckets[index]!.start)}
              </text>
            ) : null,
          )}
          <text className="trend-axis-label" x="820" y="286" textAnchor="end">
            UTC
          </text>
          {series.map((name) => (
            <polyline
              key={name}
              className={`trend-line trend-${name}`}
              vectorEffect="non-scaling-stroke"
              points={data.buckets
                .map((bucket, index) => `${x(index)},${y(bucket[name])}`)
                .join(" ")}
            />
          ))}
        </svg>
        {totals.total === 0 ? <EmptyState title={t("trends.empty")} /> : null}
      </div>
      <p className="muted">
        {t("trends.pending")}: {formatNumber(totals.pending)} · {t("trends.uncertain")}:{" "}
        {formatNumber(totals.uncertain)} · {t("trends.cancelled")}: {formatNumber(totals.cancelled)}
      </p>
      <Disclosure label={t("trends.table")}>
        <div className="table-wrap" tabIndex={0} role="region" aria-label={t("trends.table")}>
          <DataTable
            dataSource={data.buckets}
            rowKey={(bucket) => bucket.start}
            columns={[
              { title: "UTC", key: "start", render: (_value, bucket) => time(bucket.start) },
              ...(
                ["total", "succeeded", "failed", "pending", "uncertain", "cancelled"] as const
              ).map((name) => ({
                key: name,
                title: t(`trends.${name}`),
                render: (_value: unknown, bucket: Schemas["CallTrendBucket"]) =>
                  formatNumber(bucket[name]),
              })),
            ]}
          />
        </div>
      </Disclosure>
    </>
  );
}
