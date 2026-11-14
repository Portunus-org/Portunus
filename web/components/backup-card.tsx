"use client"

import * as React from "react"
import { CheckIcon, DownloadIcon, FileArchiveIcon, UploadIcon, XIcon } from "lucide-react"

import { useConfirm } from "@/components/confirm"
import { FormField } from "@/components/form-field"
import { Pane, PaneContent, PaneDescription, PaneFooter, PaneHeader, PaneTitle } from "@/components/settings-pane"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api, ApiError, errorText } from "@/lib/api"
import { formatBytes } from "@/lib/format"
import { useFormErrors } from "@/lib/form-errors"
import { useI18n } from "@/lib/i18n"
import { useSession } from "@/lib/session"
import { toast } from "@/lib/toast"

const SEALED_MAGIC = "PORTUNUS\x01"

async function isSealedArchive(file: File): Promise<boolean> {
  if (file.size < SEALED_MAGIC.length) return false
  try {
    const bytes = new Uint8Array(await file.slice(0, SEALED_MAGIC.length).arrayBuffer())
    for (let i = 0; i < SEALED_MAGIC.length; i++) {
      if (bytes[i] !== SEALED_MAGIC.charCodeAt(i)) return false
    }
    return true
  } catch {
    return false
  }
}

const ARCHIVE_ITEMS = [
  "backup.download.itemClients",
  "backup.download.itemNodes",
  "backup.download.itemCerts",
  "backup.download.itemSettings",
] as const

export function BackupCard({ onRestored }: { onRestored?: () => void }) {
  const { t, locale } = useI18n()
  const { refresh } = useSession()
  const [confirm, confirmDialog] = useConfirm()
  const { errors, clear, fail } = useFormErrors()

  const fileInputRef = React.useRef<HTMLInputElement>(null)
  const [file, setFile] = React.useState<File | null>(null)
  const [needsKey, setNeedsKey] = React.useState(false)
  const [recoveryKey, setRecoveryKey] = React.useState("")
  const [downloading, setDownloading] = React.useState(false)
  const [restoring, setRestoring] = React.useState(false)

  async function handleDownload() {
    setDownloading(true)
    try {
      const { blob, filename } = await api.downloadBackup()
      const url = URL.createObjectURL(blob)
      const a = document.createElement("a")
      a.href = url
      a.download = filename
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
    } catch (err) {
      toast.error(errorText(err, t))
    } finally {
      setDownloading(false)
    }
  }

  async function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const picked = e.target.files?.[0] ?? null
    clear()
    setFile(picked)
    if (!picked) {
      setNeedsKey(false)
      setRecoveryKey("")
      return
    }
    const sealed = await isSealedArchive(picked)
    setNeedsKey(sealed)
    if (!sealed) setRecoveryKey("")
  }

  function clearFile() {
    clear()
    setFile(null)
    setNeedsKey(false)
    setRecoveryKey("")
    if (fileInputRef.current) fileInputRef.current.value = ""
  }

  function promptRestore() {
    if (!file) return
    confirm({
      title: t("backup.confirm.title", { name: file.name }),
      description: t("backup.confirm.description"),
      action: t("backup.confirm.action"),
      onConfirm: async () => {
        setRestoring(true)
        clear()
        try {
          await api.restoreBackup(file, needsKey ? recoveryKey : undefined)
          clearFile()
          toast.success(t("backup.toast.restored"))
          await refresh()
          onRestored?.()
          document.dispatchEvent(new Event("visibilitychange"))
          setTimeout(() => document.dispatchEvent(new Event("visibilitychange")), 1200)
        } catch (err) {
          if (err instanceof ApiError && err.field === "key") {
            setNeedsKey(true)
          }
          fail(err, ["file", "key"])
        } finally {
          setRestoring(false)
        }
      },
    })
  }

  return (
    <div className="grid gap-16">
      {confirmDialog}

      <Pane>
        <PaneHeader>
          <PaneTitle>{t("backup.title")}</PaneTitle>
          <PaneDescription>{t("backup.description")}</PaneDescription>
        </PaneHeader>
        <PaneContent>
          <FormField id="backup-download" label={t("backup.download.title")} hint={t("backup.download.hint")}>
            <ul
              id="backup-download"
              data-setting="backup-download"
              className="grid gap-2 rounded-lg border bg-muted/20 px-3.5 py-3 text-xs text-muted-foreground"
            >
              {ARCHIVE_ITEMS.map((item) => (
                <li key={item} className="flex items-start gap-2">
                  <CheckIcon className="mt-0.5 size-3.5 shrink-0 text-foreground/70" />
                  <span>{t(item)}</span>
                </li>
              ))}
            </ul>
          </FormField>
        </PaneContent>
        <PaneFooter className="border-t">
          <Button size="sm" onClick={handleDownload} disabled={downloading || restoring}>
            <DownloadIcon data-icon="inline-start" />
            {downloading ? t("backup.downloading") : t("backup.download.button")}
          </Button>
        </PaneFooter>
      </Pane>

      <Pane>
        <PaneHeader>
          <PaneTitle>{t("backup.restore.title")}</PaneTitle>
          <PaneDescription>{t("backup.restore.description")}</PaneDescription>
        </PaneHeader>
        <PaneContent>
          <FormField id="backup-file" label={t("backup.restore.file")} hint={t("backup.restore.hint")} error={errors.file}>
            <div className="grid min-w-0 gap-2">
              <input
                ref={fileInputRef}
                id="backup-file"
                data-setting="backup-file"
                type="file"
                accept=".tgz,.tar.gz,.gz,.enc,application/gzip,application/x-gzip,application/octet-stream"
                className="sr-only"
                onChange={handleFileChange}
              />
              {file ? (
                <div className="grid min-w-0 gap-2 rounded-lg border bg-muted/20 p-3">
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <FileArchiveIcon className="size-4 shrink-0 text-muted-foreground" />
                      <span className="truncate font-mono text-xs">{file.name}</span>
                    </div>
                    <Button
                      type="button"
                      size="icon-xs"
                      variant="ghost"
                      className="shrink-0"
                      aria-label={t("common.close")}
                      onClick={clearFile}
                      disabled={restoring}
                    >
                      <XIcon className="size-3.5" />
                    </Button>
                  </div>
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs text-muted-foreground tabular-nums">{formatBytes(file.size, locale)}</span>
                    <Button
                      type="button"
                      size="xs"
                      variant="outline"
                      onClick={() => fileInputRef.current?.click()}
                      disabled={restoring}
                    >
                      {t("backup.restore.change")}
                    </Button>
                  </div>
                </div>
              ) : (
                <button
                  type="button"
                  data-setting="backup-choose"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={restoring || downloading}
                  aria-invalid={!!errors.file || undefined}
                  className="flex min-h-9 w-full items-center justify-between gap-3 rounded-lg border bg-background px-3 py-2 text-left text-sm transition-colors hover:bg-accent/50 disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive"
                >
                  <span className="flex min-w-0 items-center gap-2 text-muted-foreground">
                    <FileArchiveIcon className="size-4 shrink-0" />
                    <span className="truncate">{t("backup.restore.empty")}</span>
                  </span>
                  <span className="shrink-0 text-xs font-medium text-foreground">{t("backup.restore.choose")}</span>
                </button>
              )}
            </div>
          </FormField>

          {needsKey && (
            <FormField
              id="backup-key"
              label={t("backup.restore.key")}
              hint={t("backup.restore.keyHint")}
              error={errors.key}
            >
              <Input
                id="backup-key"
                data-setting="backup-key"
                value={recoveryKey}
                onChange={(e) => (setRecoveryKey(e.target.value), clear())}
                placeholder={t("backup.restore.keyPlaceholder")}
                className="font-mono"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                aria-invalid={!!errors.key}
              />
            </FormField>
          )}
        </PaneContent>
        <PaneFooter className="border-t">
          <Button
            type="button"
            size="sm"
            data-setting="backup-restore"
            onClick={promptRestore}
            disabled={!file || restoring || downloading}
          >
            <UploadIcon data-icon="inline-start" />
            {restoring ? t("backup.restoring") : t("backup.restore.button")}
          </Button>
        </PaneFooter>
      </Pane>
    </div>
  )
}
