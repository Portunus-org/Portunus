"use client"

import { Button } from "@/components/ui/button"
import { useI18n } from "@/lib/i18n"

// Shown instead of a blank page when a panel screen throws while rendering.
export default function PanelError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  const { t } = useI18n()
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 py-24 text-center">
      <h2 className="text-lg font-semibold">{t("renderError.title")}</h2>
      <p className="max-w-md font-mono text-xs break-all text-muted-foreground">{error.message}</p>
      <Button variant="outline" onClick={reset}>
        {t("common.retry")}
      </Button>
    </div>
  )
}
