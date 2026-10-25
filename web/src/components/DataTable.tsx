/** Shared Semi table defaults; server cursor retrieval remains owned by each page. */
import Table from "@douyinfe/semi-ui/lib/es/table";
import type { ColumnProps } from "@douyinfe/semi-ui/lib/es/table";
import type { ReactNode } from "react";
import { COMPACT_QUERY, useMediaQuery } from "../lib/useMediaQuery";
import { useI18n } from "../lib/i18n";

/**
 * Paginate only the records already loaded, without implying a server-wide total. On
 * compact widths a page may supply `mobileRow` to recompose each record as a stacked item.
 *
 * @param props - Records, columns, key function and optional compact renderer.
 * @returns Semi table or stacked record list.
 */
export function DataTable<Record extends object>({
  dataSource,
  columns,
  rowKey,
  mobileRow,
}: {
  dataSource: Record[];
  columns: ColumnProps<Record>[];
  rowKey: (record: Record) => string;
  mobileRow?: (record: Record) => ReactNode;
}) {
  const isCompact = useMediaQuery(COMPACT_QUERY);
  const { t } = useI18n();
  if (isCompact && mobileRow) {
    return (
      <ul className="mobile-records">
        {dataSource.map((record) => (
          <li key={rowKey(record)}>{mobileRow(record)}</li>
        ))}
      </ul>
    );
  }
  return (
    <>
      {isCompact ? <p className="table-scroll-hint">{t("common.scrollTable")}</p> : null}
      <Table<Record>
        dataSource={dataSource}
        columns={columns}
        rowKey={(record) => (record === undefined ? "" : rowKey(record))}
        pagination={dataSource.length > 50 ? { pageSize: 50, showSizeChanger: false } : false}
        size="middle"
      />
    </>
  );
}
