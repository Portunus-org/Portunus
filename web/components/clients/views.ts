import type { TableView } from "@/components/data-table"
import type { Client } from "@/lib/api"

type Translate = (key: "clients.view.all" | "clients.view.active" | "clients.view.attention" | "clients.view.disabled") => string

const filters: Record<string, (c: Client) => boolean> = {
  all: () => true,
  active: (c) => c.status === "active",
  attention: (c) => c.status === "expired" || c.status === "limited",
  disabled: (c) => c.status === "disabled",
}

/** The tabs over a table of clients, with how many clients each holds. */
export function clientViews(clients: Client[], t: Translate): TableView[] {
  const count = (view: string) => clients.filter(filters[view]).length
  return [
    { value: "all", label: t("clients.view.all") },
    { value: "active", label: t("clients.view.active"), count: count("active") },
    { value: "attention", label: t("clients.view.attention"), count: count("attention") },
    { value: "disabled", label: t("clients.view.disabled"), count: count("disabled") },
  ]
}

/** The clients of a view that match what was typed into the search. */
export function filterClients(clients: Client[], view: string, query: string): Client[] {
  const q = query.trim().toLowerCase()
  const inView = filters[view] ?? filters.all
  return clients.filter((c) => inView(c) && (!q || c.name.toLowerCase().includes(q) || c.note.toLowerCase().includes(q)))
}
