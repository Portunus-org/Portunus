import { cn } from "@/lib/utils"

/**
 * The Portunus mark: three nested archways, a chain of gateways seen
 * head-on. Drawn on Lucide's 24px grid so it sits naturally next to icons.
 */
export const ARCH_PATHS = ["M3 21V10a9 9 0 0 1 18 0v11", "M6.5 18.5v-7a5.5 5.5 0 0 1 11 0v7", "M9.5 16.5v-4a2.5 2.5 0 0 1 5 0v4"] as const

export function LogoMark({ className, ...props }: React.ComponentProps<"svg">) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={cn("size-5 shrink-0", className)}
      {...props}
    >
      {ARCH_PATHS.map((d) => (
        <path key={d} d={d} />
      ))}
    </svg>
  )
}
