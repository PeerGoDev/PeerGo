import type { ReactNode } from "react"

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"

export interface StaffSettingsValueRow {
  label: ReactNode
  value: ReactNode
}

export function StaffSettingsValueTable({
  rows,
  valueHeader = "当前值",
}: {
  rows: StaffSettingsValueRow[]
  valueHeader?: string
}) {
  return (
    <Table containerClassName="px-3">
      <TableHeader>
        <TableRow>
          <TableHead>项目</TableHead>
          <TableHead className="text-right">{valueHeader}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row, index) => (
          <TableRow key={index}>
            <TableCell className="text-muted-foreground">{row.label}</TableCell>
            <TableCell className="text-right font-medium tabular-nums">
              {row.value}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
