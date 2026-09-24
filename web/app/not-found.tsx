import Link from "next/link"

import { LogoMark } from "@/components/brand/logo"

export default function NotFound() {
  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-6 px-6 text-center">
      <LogoMark className="size-10 text-muted-foreground" />
      <div className="flex flex-col gap-2">
        <h1 className="font-display text-3xl">Nothing behind this gate.</h1>
        <p className="text-sm text-muted-foreground">The page you asked for does not exist.</p>
      </div>
      <Link href="/" className="text-sm underline underline-offset-4">
        Back to the panel
      </Link>
    </main>
  )
}
