"use client"

import * as React from "react"
import {
  columnVisibilityFeature,
  createColumnHelper,
  createPaginatedRowModel,
  createSortedRowModel,
  FlexRender,
  rowPaginationFeature,
  rowSortingFeature,
  tableFeatures,
  useTable,
  type ColumnDef,
  type ColumnVisibilityState,
  type RowData,
  type SortingState,
} from "@tanstack/react-table"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
  Columns3Icon,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useI18n } from "@/lib/i18n"
import { cn } from "@/lib/utils"

// Features used by every table in the panel (TanStack Table v9 registers
// them explicitly; unused ones are tree-shaken).
export const tableFeatureSet = tableFeatures({
  columnVisibilityFeature,
  rowPaginationFeature,
  rowSortingFeature,
  paginatedRowModel: createPaginatedRowModel(),
  sortedRowModel: createSortedRowModel(),
})

export type TableFeatures = typeof tableFeatureSet

export function columnHelper<T extends RowData>() {
  return createColumnHelper<TableFeatures, T>()
}

export interface TableView {
  value: string
  label: string
  count?: number
}

/**
 * The dashboard-01 data table, generalised: view tabs (a select on narrow
 * screens), column visibility, sorting, pagination and a toolbar slot.
 */
export function DataTable<T extends RowData>({
  data,
  columns,
  getRowId,
  views,
  view,
  onViewChange,
  toolbar,
  empty,
  loading = false,
  columnLabels = {},
  initialSorting = [],
  initialHidden = [],
  pageSize = 20,
  pager = "auto",
  found,
}: {
  data: T[]
  columns: ColumnDef<TableFeatures, T, any>[] // eslint-disable-line @typescript-eslint/no-explicit-any
  getRowId: (row: T) => string
  views?: TableView[]
  view?: string
  onViewChange?: (v: string) => void
  toolbar?: React.ReactNode
  empty?: React.ReactNode
  /** The rows are still on their way: their shapes are shown instead of "nothing here". */
  loading?: boolean
  /** Human names for the column visibility menu, by column id. */
  columnLabels?: Record<string, string>
  initialSorting?: SortingState
  /** Columns that are not shown until chosen in the columns menu, by id. */
  initialHidden?: string[]
  /** Rows on a page to begin with. */
  pageSize?: number
  /** The page controls under the table: only when there is a second page, or always. */
  pager?: "auto" | "always"
  /** A row to show: its page is turned to, it is scrolled to and lights up once, as a setting found in the settings does. */
  found?: string | null
}) {
  const { t, tp } = useI18n()
  const [columnVisibility, setColumnVisibility] = React.useState<ColumnVisibilityState>(() =>
    Object.fromEntries(initialHidden.map((id) => [id, false]))
  )
  const [sorting, setSorting] = React.useState<SortingState>(initialSorting)
  const [pagination, setPagination] = React.useState({
    pageIndex: 0,
    pageSize,
  })

  const table = useTable({
    features: tableFeatureSet,
    data,
    columns,
    state: { sorting, columnVisibility, pagination },
    getRowId,
    onSortingChange: setSorting,
    onColumnVisibilityChange: setColumnVisibility,
    onPaginationChange: setPagination,
  })

  // The row asked for, once it is among the data: the page it falls on in
  // the present order, then the row itself, lit up for a moment.
  const [lit, setLit] = React.useState<string | null>(null)
  const shown = React.useRef<string | null>(null)
  React.useEffect(() => {
    if (!found || shown.current === found) return
    const at = table.getPrePaginatedRowModel().rows.findIndex((r) => r.id === found)
    if (at < 0) return
    shown.current = found
    const frame = requestAnimationFrame(() => {
      setPagination((p) => ({ ...p, pageIndex: Math.floor(at / p.pageSize) }))
      setLit(found)
    })
    return () => cancelAnimationFrame(frame)
  }, [found, data, table])
  React.useEffect(() => {
    if (!lit) return
    document.querySelector(`[data-row-id="${CSS.escape(lit)}"]`)?.scrollIntoView({ block: "center", behavior: "smooth" })
    const off = setTimeout(() => setLit(null), 1800)
    return () => clearTimeout(off)
  }, [lit])

  // Back to the first page when the view changes the data underneath.
  const changeView = (v: string) => {
    setPagination((p) => ({ ...p, pageIndex: 0 }))
    onViewChange?.(v)
  }

  const hideable = table.getAllColumns().filter((c) => typeof c.accessorFn !== "undefined" && c.getCanHide())

  return (
    <div className="flex w-full flex-col gap-4">
      <div className="flex items-center justify-between gap-2 px-4 lg:px-6">
        {views && view && onViewChange ? (
          <Tabs value={view} onValueChange={changeView}>
            <Label htmlFor="view-selector" className="sr-only">
              {t("common.view")}
            </Label>
            <Select value={view} onValueChange={changeView}>
              <SelectTrigger className="flex w-fit @4xl/main:hidden" size="sm" id="view-selector">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {views.map((v) => (
                    <SelectItem key={v.value} value={v.value}>
                      {v.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <TabsList className="hidden **:data-[slot=badge]:size-5 **:data-[slot=badge]:rounded-full **:data-[slot=badge]:bg-muted-foreground/30 **:data-[slot=badge]:px-1 @4xl/main:flex">
              {views.map((v) => (
                <TabsTrigger key={v.value} value={v.value}>
                  {v.label}
                  {v.count ? <Badge variant="secondary">{v.count}</Badge> : null}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        ) : (
          <div />
        )}
        <div className="flex items-center gap-2">
          {hideable.length > 0 && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm">
                  <Columns3Icon data-icon="inline-start" />
                  <span className="sr-only lg:not-sr-only">{t("common.columns")}</span>
                  <ChevronDownIcon data-icon="inline-end" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-40">
                {hideable.map((column) => (
                  <DropdownMenuCheckboxItem
                    key={column.id}
                    checked={column.getIsVisible()}
                    onCheckedChange={(value) => column.toggleVisibility(!!value)}
                  >
                    {columnLabels[column.id] ?? column.id}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          {toolbar}
        </div>
      </div>

      <div className="flex flex-col gap-4 px-4 lg:px-6">
        <div className="overflow-hidden rounded-lg border">
          <Table>
            <TableHeader className="sticky top-0 z-10 bg-muted">
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id} className="hover:bg-transparent">
                  {group.headers.map((header) => {
                    const sorted = header.column.getIsSorted()
                    return (
                      <TableHead key={header.id} colSpan={header.colSpan}>
                        {header.isPlaceholder ? null : header.column.getCanSort() ? (
                          <button
                            type="button"
                            onClick={header.column.getToggleSortingHandler()}
                            className="inline-flex items-center gap-1 hover:text-foreground"
                          >
                            <FlexRender header={header} />
                            {sorted === "asc" && <ArrowUpIcon className="size-3" />}
                            {sorted === "desc" && <ArrowDownIcon className="size-3" />}
                          </button>
                        ) : (
                          <FlexRender header={header} />
                        )}
                      </TableHead>
                    )
                  })}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {loading ? (
                // shadcn's Skeleton, in the shape of the rows to come: the
                // table keeps its height and nothing jumps when they arrive.
                Array.from({ length: 4 }, (_, i) => (
                  <TableRow key={i} className="hover:bg-transparent">
                    {table.getVisibleLeafColumns().map((column, j) => (
                      <TableCell key={column.id}>
                        {column.id !== "actions" && <Skeleton className="h-4" style={{ width: `${[70, 45, 55, 35][(i + j) % 4]}%` }} />}
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : table.getRowModel().rows.length ? (
                table.getRowModel().rows.map((row) => (
                  <TableRow key={row.id} data-row-id={row.id} className={cn(lit === row.id && "row-found")}>
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id} className={cn(cell.column.id === "actions" && "w-10 text-right")}>
                        <FlexRender cell={cell} />
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={columns.length} className="h-32 text-center text-muted-foreground">
                    {empty ?? t("common.noResults")}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>

        {(pager === "always" || table.getPageCount() > 1) && (
          <div className="flex items-center justify-between px-2">
            <div className="hidden flex-1 text-sm text-muted-foreground lg:flex">{tp("common.rows", data.length)}</div>
            <div className="flex w-full items-center gap-8 lg:w-fit">
              <div className="hidden items-center gap-2 lg:flex">
                <Label htmlFor="rows-per-page" className="text-sm font-medium">
                  {t("common.rowsPerPage")}
                </Label>
                <Select value={`${pagination.pageSize}`} onValueChange={(v) => table.setPageSize(Number(v))}>
                  <SelectTrigger size="sm" className="w-20" id="rows-per-page">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent side="top">
                    <SelectGroup>
                      {[10, 20, 50, 100].map((n) => (
                        <SelectItem key={n} value={`${n}`}>
                          {n}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
              <div className="flex w-fit items-center justify-center text-sm font-medium tabular-nums">
                {t("common.page", {
                  page: pagination.pageIndex + 1,
                  pages: Math.max(1, table.getPageCount()),
                })}
              </div>
              <div className="ml-auto flex items-center gap-2 lg:ml-0">
                <Button
                  variant="outline"
                  size="icon"
                  className="hidden size-8 lg:flex"
                  onClick={() => table.setPageIndex(0)}
                  disabled={!table.getCanPreviousPage()}
                >
                  <span className="sr-only">First page</span>
                  <ChevronsLeftIcon />
                </Button>
                <Button
                  variant="outline"
                  size="icon"
                  className="size-8"
                  onClick={() => table.previousPage()}
                  disabled={!table.getCanPreviousPage()}
                >
                  <span className="sr-only">Previous page</span>
                  <ChevronLeftIcon />
                </Button>
                <Button
                  variant="outline"
                  size="icon"
                  className="size-8"
                  onClick={() => table.nextPage()}
                  disabled={!table.getCanNextPage()}
                >
                  <span className="sr-only">Next page</span>
                  <ChevronRightIcon />
                </Button>
                <Button
                  variant="outline"
                  size="icon"
                  className="hidden size-8 lg:flex"
                  onClick={() => table.setPageIndex(table.getPageCount() - 1)}
                  disabled={!table.getCanNextPage()}
                >
                  <span className="sr-only">Last page</span>
                  <ChevronsRightIcon />
                </Button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
