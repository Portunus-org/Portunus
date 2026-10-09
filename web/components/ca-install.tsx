"use client"

import * as React from "react"
import { DownloadIcon } from "lucide-react"

import { QR } from "@/components/clients/client-drawer"
import { CopyButton } from "@/components/copy-button"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, type CAInfo } from "@/lib/api"
import { formatDate } from "@/lib/format"
import { useI18n } from "@/lib/i18n"

const platforms = [
  ["ios", "ca.ios", "ca.iosSteps", null],
  ["android", "ca.android", "ca.androidSteps", "ca.androidNote"],
  ["windows", "ca.windows", "ca.windowsSteps", null],
  ["macos", "ca.macos", "ca.macosSteps", null],
] as const

/**
 * Everything needed to install the panel's CA on devices: downloads, a QR
 * code for phones, the fingerprint to compare, and per-platform steps.
 */
export function CAInstall({ compact = false }: { compact?: boolean }) {
  const { t, locale } = useI18n()
  const [ca, setCa] = React.useState<CAInfo | null>(null)

  React.useEffect(() => {
    api.ca().then(setCa, () => {})
  }, [])

  return (
    <div className="grid gap-5 text-sm">
      {!compact && <p className="text-muted-foreground">{t("ca.lead")}</p>}
      <div className="flex flex-col gap-5 sm:flex-row sm:items-start">
        {ca?.downloadQr && (
          <div className="grid shrink-0 justify-items-center gap-2 text-center">
            <QR svg={ca.downloadQr} label={t("ca.qr")} className="max-w-40" />
            <span className="max-w-40 text-xs text-muted-foreground">{t("ca.qr")}</span>
          </div>
        )}
        <div className="grid min-w-0 flex-1 gap-4">
          <div className="flex flex-wrap gap-2">
            <Button asChild variant="outline" size="sm">
              <a href={api.caCertificateUrl("pem")} download>
                <DownloadIcon />
                {t("ca.download")}
              </a>
            </Button>
            <Button asChild variant="outline" size="sm">
              <a href={api.caCertificateUrl("der")} download>
                <DownloadIcon />
                {t("ca.downloadDer")}
              </a>
            </Button>
          </div>
          {ca && (
            <div className="grid gap-1">
              <span>{ca.subject}</span>
              <span className="text-muted-foreground">{t("ca.expires", { date: formatDate(ca.notAfter, locale) })}</span>
              <span className="text-muted-foreground">{t("ca.fingerprint")}</span>
              <span className="flex items-start gap-1">
                <code className="min-w-0 font-mono text-xs break-all">{ca.fingerprint.match(/.{2}/g)?.join(":")}</code>
                <CopyButton value={ca.fingerprint} />
              </span>
              <span className="text-xs text-muted-foreground">{t("ca.fingerprintHint")}</span>
            </div>
          )}
        </div>
      </div>
      <Tabs defaultValue="ios">
        <TabsList>
          {platforms.map(([id, label]) => (
            <TabsTrigger key={id} value={id}>
              {t(label)}
            </TabsTrigger>
          ))}
        </TabsList>
        {platforms.map(([id, , steps, note]) => (
          <TabsContent key={id} value={id} className="grid gap-3 pt-1">
            {/* One step per line of the message. */}
            <ol className="grid list-decimal gap-1.5 pl-5 marker:text-muted-foreground">
              {t(steps)
                .split("\n")
                .map((step, i) => (
                  <li key={i} className="pl-1">
                    {step}
                  </li>
                ))}
            </ol>
            {note && <p className="text-muted-foreground">{t(note)}</p>}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}
