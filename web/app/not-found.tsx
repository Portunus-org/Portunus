"use client"

import Link from "next/link"

import { LogoMark } from "@/components/brand/logo"
import { Button } from "@/components/ui/button"
import { useI18n } from "@/lib/i18n"

export default function NotFound() {
  const { t } = useI18n()
  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-6 bg-muted p-6 text-center">
      <LogoMark className="size-10 text-muted-foreground" />
      <div className="grid gap-2">
        <h1 className="text-xl font-semibold">{t("notFound.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("notFound.text")}</p>
      </div>
      <Button asChild variant="outline">
        <Link href="/">{t("notFound.back")}</Link>
      </Button>
    </main>
  )
}
