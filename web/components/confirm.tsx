"use client"

import * as React from "react"

import { Trash2Icon } from "lucide-react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { useI18n } from "@/lib/i18n"

export interface ConfirmRequest {
  title: string
  description: string
  action: string
  destructive?: boolean
  onConfirm: () => Promise<void> | void
}

/**
 * Imperative confirmation dialog: `const [confirm, dialog] = useConfirm()`.
 * Destructive requests follow shadcn's destructive alert dialog: compact,
 * with a media icon and a destructive action button.
 */
export function useConfirm(): [(r: ConfirmRequest) => void, React.ReactNode] {
  const { t } = useI18n()
  const [req, setReq] = React.useState<ConfirmRequest | null>(null)
  const [pending, setPending] = React.useState(false)
  // Keeps the last request rendered while the dialog animates out.
  const [shown, setShown] = React.useState<ConfirmRequest | null>(null)
  if (req && req !== shown) setShown(req)

  const dialog = (
    <AlertDialog open={!!req} onOpenChange={(o) => !o && !pending && setReq(null)}>
      <AlertDialogContent size={shown?.destructive ? "sm" : "default"}>
        <AlertDialogHeader>
          {shown?.destructive && (
            <AlertDialogMedia className="bg-destructive/10 text-destructive dark:bg-destructive/20 dark:text-destructive">
              <Trash2Icon />
            </AlertDialogMedia>
          )}
          <AlertDialogTitle>{shown?.title}</AlertDialogTitle>
          <AlertDialogDescription>{shown?.description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            disabled={pending}
            variant={shown?.destructive ? "destructive" : "default"}
            onClick={async (e) => {
              e.preventDefault()
              if (!req) return
              setPending(true)
              try {
                await req.onConfirm()
                setReq(null)
              } finally {
                setPending(false)
              }
            }}
          >
            {shown?.action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
  return [setReq, dialog]
}
