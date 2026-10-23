/** Shared Semi table defaults; server cursor retrieval remains owned by each page. */
import Table from "@douyinfe/semi-ui/lib/es/table";
import type { ColumnProps } from "@douyinfe/semi-ui/lib/es/table";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";

/** Paginate only the records already loaded, without implying a server-wide total. */
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
  const [isNarrow, setNarrow] = useState(
    () => window.matchMedia?.("(max-width: 640px)").matches ?? false,
  );
  useEffect(() => {
    const media = window.matchMedia?.("(max-width: 640px)");
    if (!media) return;
    const update = () => setNarrow(media.matches);
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  if (isNarrow && mobileRow) {
    return (
      <ul className="mobile-records">
        {dataSource.map((record) => (
          <li key={rowKey(record)}>{mobileRow(record)}</li>
        ))}
      </ul>
    );
  }
  return (
    <Table<Record>
      dataSource={dataSource}
      columns={columns}
      rowKey={(record) => (record === undefined ? "" : rowKey(record))}
      pagination={dataSource.length > 50 ? { pageSize: 50, showSizeChanger: false } : false}
      size="middle"
    />
  );
}
